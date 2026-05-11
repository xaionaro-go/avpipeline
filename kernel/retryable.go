// retryable.go implements a wrapper kernel that automatically retries underlying kernel operations on failure.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

type Retryable[K Abstract] struct {
	*closuresignaler.ClosureSignaler
	Factory           func(context.Context) (K, error)
	OnError           RetryableFuncOnError[K]
	Config            RetryableConfig[K]
	Kernel            K
	KernelIsSet       bool
	KernelLocker      xsync.CtxLocker
	KernelError       error
	KernelOpenBarrier xatomic.Pointer[chan struct{}]
	lifecycleCtx      context.Context
	lifecycleCancel   context.CancelFunc
}

func NewRetryable[K Abstract](
	ctx context.Context,
	factory func(context.Context) (K, error),
	onErrorFunc RetryableFuncOnError[K],
	opts ...RetryableOption[K],
) *Retryable[K] {
	lifecycleCtx, lifecycleCancel := context.WithCancel(xcontext.DetachDone(ctx))
	r := &Retryable[K]{
		ClosureSignaler: closuresignaler.New(),
		Factory:         factory,
		OnError:         onErrorFunc,
		Config:          RetryableOptions[K](opts).Config(),
		KernelLocker:    make(xsync.CtxLocker, 1),
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
	}
	r.KernelOpenBarrier.Pointer = ptr(make(chan struct{}))
	if r.Config.StartOnInit {
		r.unpauseKernelOpening(ctx)
		if r.Config.OnInit != nil {
			r.Config.OnInit(ctx, r)
		}
		// Detach the spawned goroutine's ctx from the caller's ctx for
		// the same reason as Retryable.Unpause (see commentary there):
		// the Retryable's lifecycle is governed by its own
		// ClosureSignaler, so cancellation of a request-scoped caller
		// (e.g. gRPC RPC ctx that's cancelled on RPC return) must not
		// propagate to this long-lived openKernelIfNeeded goroutine.
		// Without the detach, NewRetryable callers from RPC handlers —
		// most notably ffstream's senderFactory.newOutputWithRetry,
		// which creates the output kernel from inside
		// StreamMux.SwitchToOutputByProps invoked off the gRPC
		// SwitchOutputByProps RPC ctx — would have their freshly-
		// created Retryable wedged the moment the RPC returns.
		observability.Go(r.lifecycleCtx, func(ctx context.Context) {
			r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
				r.openKernelIfNeeded(ctx)
			})
		})
	} else {
		r.pauseKernelOpening(ctx)
		if r.Config.OnInit != nil {
			r.Config.OnInit(ctx, r)
		}
	}
	return r
}

var (
	_ Abstract                    = (*Retryable[Abstract])(nil)
	_ packet.Source               = (*Retryable[Abstract])(nil)
	_ packet.Sink                 = (*Retryable[Abstract])(nil)
	_ types.OriginalPacketSourcer = (*Retryable[Abstract])(nil)
)

// OriginalPacketSource returns the underlying kernel as a packet.Source if it
// implements that interface and is currently available. This allows downstream
// code to get the actual source kernel rather than the Retryable wrapper.
func (r *Retryable[K]) OriginalPacketSource() packet.Source {
	ctx := context.Background()
	snap := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() kernelSnapshot[K] {
		return kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	})
	if !snap.isSet {
		return nil
	}
	if src, ok := any(snap.kernel).(packet.Source); ok {
		return src
	}
	return nil
}

func (r *Retryable[K]) unpauseKernelOpening(
	ctx context.Context,
) {
	oldCh := r.KernelOpenBarrier.Load()
	// close the channel if opened:
	select {
	case <-ctx.Done():
		return
	case <-*oldCh:
	default:
		close(*oldCh)
	}
}

func (r *Retryable[K]) pauseKernelOpening(
	ctx context.Context,
) {
	// open the channel if closed:
	newCh := make(chan struct{})
	for {
		oldCh := r.KernelOpenBarrier.Load()
		select {
		case <-ctx.Done():
			return
		case <-*oldCh:
			if r.KernelOpenBarrier.CompareAndSwap(oldCh, &newCh) {
				return
			}
			runtime.Gosched()
		default:
			return
		}
	}
}

func (r *Retryable[K]) openKernelIfNeeded(
	ctx context.Context,
) {
	if r.KernelIsSet || r.KernelError != nil {
		return
	}
	logger.Debugf(ctx, "openKernelIfNeeded")
	defer logger.Debugf(ctx, "/openKernelIfNeeded")

	for {

		// Release KernelLocker around the barrier wait. While paused
		// the barrier channel is open (no value to receive), so the
		// select below blocks indefinitely until Unpause flips the
		// barrier. Holding KernelLocker for the duration of that wait
		// starves Pause / Close (and any other control op that needs
		// the lock briefly) — concurrent FromKernel.Generate loops on
		// empty fallback chains were observed deadlocked at
		// `inputwithfallback.Serve.func5.1 → InputChain.Pause →
		// Retryable.Pause` for 8+ minutes against this barrier wait,
		// which in turn wedged the InputSwitch's switchingProcN
		// counter and blocked subsequent fallback switches with
		// "another switch is in progress".
		//
		// We re-acquire with context.Background() because the outer
		// caller (NewRetryable startup goroutine, Unpause.func1, and
		// retry() via getKernel) all expect the lock to be held when
		// openKernelIfNeeded returns; the caller's ctx may already be
		// cancelled by then but the deferred unlock still needs a
		// matching lock. Any concurrent grabber (Pause, Unpause,
		// Close) holds the lock only briefly, so the re-acquire is
		// not a starvation hazard.
		//
		// We carry the WithEnableDeadlock(false) tag through to the
		// re-lock context: every caller above wraps the outer
		// KernelLocker.Do with WithEnableDeadlock(ctx, false), so the
		// re-acquire must match — re-locking with a bare Background
		// would re-enable the xsync deadlock detector inside an
		// outer-disabled scope and surface false positives across the
		// already-known KernelLocker → InputChainsLocker hand-off.
		bgNoDeadlock := xsync.WithEnableDeadlock(context.Background(), false)
		r.KernelLocker.ManualUnlock(ctx)
		select {
		case <-ctx.Done():
			_ = r.KernelLocker.ManualLock(bgNoDeadlock)
			logger.Errorf(ctx, "unable to open the kernel, because we are finishing: %v", ctx.Err())
			if r.KernelError == nil {
				r.KernelError = ctx.Err()
			}
			return
		case <-r.ClosureSignaler.CloseChan():
			_ = r.KernelLocker.ManualLock(bgNoDeadlock)
			logger.Errorf(ctx, "unable to open the kernel, because the retryable is being closed")
			if r.KernelError == nil {
				r.KernelError = io.EOF
			}
			return
		case <-*r.KernelOpenBarrier.Load():
			// barrier is open (closed channel) — proceed.
		}
		_ = r.KernelLocker.ManualLock(bgNoDeadlock)

		// Re-check the early-exit conditions after re-acquiring the
		// lock: a concurrent Pause or Close may have set KernelError
		// or installed a kernel while we were waiting, in which case
		// we must not run Factory.
		if r.KernelIsSet || r.KernelError != nil {
			return
		}

		openCtx, cancelOpen := r.openAttemptContext(ctx)
		k, err := r.Factory(openCtx)
		if err == nil {
			if stopErr := r.openAttemptStopped(openCtx); stopErr != nil {
				cancelOpen()
				r.discardOpenedKernel(ctx, k, stopErr)
				return
			}
		}
		if err != nil {
			if stopErr := r.openAttemptStopped(openCtx); stopErr != nil {
				cancelOpen()
				if r.KernelError == nil {
					r.KernelError = stopErr
				}
				return
			}
		}
		logger.Debugf(ctx, "factory results: %p %v", k, err)
		if err == nil {
			if r.Config.OnKernelOpen != nil {
				// Release KernelLocker around OnKernelOpen. The
				// OnKernelOpen callback registered by
				// preset/inputwithfallback calls
				// resetDownstreamKernels, which forwards to
				// Decoder.Reset → Decoder.ResetHard. ResetHard acquires
				// the kernel.Decoder.Locker; for each per-stream entry
				// it then nests into codec.Decoder.locker via
				// decoder.LockDo. If a hardware codec is wedged inside
				// avcodec_send_packet (silent-consume stall on
				// h264_mediacodec / av1_mediacodec), the per-stream
				// codec.Decoder.locker is held indefinitely. Holding
				// KernelLocker across that wait would propagate the
				// stall to the entire Retryable: every Pause / Unpause
				// / Generate / SendInput / control-RPC queues behind
				// the KernelLocker forever, freezing the chain.
				//
				// Releasing KernelLocker for the duration of
				// OnKernelOpen breaks the lock-order chain: only that
				// one OnKernelOpen call wedges; the rest of the
				// Retryable remains responsive. A containment timeout
				// in resetDownstreamKernels caps the wedge duration to
				// bound the impact further.
				//
				// SAFETY: while we release the lock, a concurrent
				// opener (parallel openKernelIfNeeded from
				// retry/Generate/Unpause/NewRetryable startup) may run
				// its own factory and successfully install ITS kernel.
				// On re-acquire we re-check r.KernelIsSet — if true,
				// our k is an orphan and must be closed to free CGo
				// resources. We also re-check r.KernelError so a
				// concurrent Close+Pause race installs a final error
				// rather than us silently overwriting it.
				var onOpenErr error
				r.withKernelLockerReleased(ctx, func() {
					onOpenErr = r.Config.OnKernelOpen(openCtx, k)
				})
				if stopErr := r.openAttemptStopped(openCtx); stopErr != nil {
					cancelOpen()
					r.discardOpenedKernel(ctx, k, stopErr)
					return
				}
				if r.KernelIsSet || r.KernelError != nil {
					cancelOpen()
					logger.Debugf(ctx, "concurrent open/close won the race during OnKernelOpen; closing orphan kernel")
					r.closeOpenedKernel(ctx, k, "orphan kernel after concurrent open/close")
					return
				}
				if onOpenErr != nil {
					cancelOpen()
					r.KernelError = onOpenErr
					r.Close(ctx)
					return
				}
			}
			logger.Debugf(ctx, "set kernel")
			r.Kernel = k
			r.KernelIsSet = true
			cancelOpen()
			// If Pause was called while the kernel was being opened,
			// honour that pause intent now. Otherwise the caller's pause
			// would silently be lost. We detect this by checking the
			// barrier directly (non-blocking): if it is paused (channel
			// open), close the freshly-opened kernel.
			select {
			case <-*r.KernelOpenBarrier.Load():
				// barrier is open (closed channel) — fine, continue
			default:
				logger.Debugf(ctx, "pause was requested during kernel open; closing kernel")
				var zeroValue K
				r.closeOpenedKernel(ctx, r.Kernel, "kernel while applying deferred pause")
				r.Kernel = zeroValue
				r.KernelIsSet = false
			}
			return
		}

		if r.OnError == nil {
			cancelOpen()
			r.KernelError = err
			r.Close(ctx)
			return
		}

		// Release KernelLocker around OnError. The OnError callback
		// (typically inputwithfallback.onInputChainError) sleeps
		// `RetryInterval` between factory-error retries to back off
		// the failing source. Holding KernelLocker across that sleep
		// starves concurrent control operations (Pause, Unpause,
		// AddInput → chain.Pause+Unpause) — they queue indefinitely
		// behind a goroutine that is just sleeping. The OnError
		// callback does not access the Retryable's locked fields
		// (Kernel/KernelIsSet/KernelError), so dropping the lock
		// across it is safe. See withKernelLockerReleased's doc for
		// the relock-with-bg rationale.
		r.withKernelLockerReleased(ctx, func() {
			err = r.OnError(openCtx, k, err)
		})
		cancelOpen()
		switch {
		case err == nil:
		case errors.As(err, &ErrRetry{}):
		default:
			r.KernelError = err
			r.Close(ctx)
			return
		}
	}
}

func (r *Retryable[K]) retry(
	ctx context.Context,
	callback func(K) error,
) (_err error) {
	// retry must release KernelLocker around the long-running callback
	// so that concurrent control operations (Pause, Unpause,
	// OriginalPacketSource, ...) can acquire it. Previously this used
	// xsync.DoR1 with WithAllowUnlockNotLocked + an inner
	// ManualUnlock+deferred-ManualLock pattern: when the deferred
	// ManualLock failed (ctx cancelled), the outer Do's deferred
	// ManualUnlock still ran and *received from the channel*,
	// stealing the lock from whichever goroutine had legitimately
	// re-acquired it in the meantime — leading to "not locked!"
	// panics in unrelated callers later.
	//
	// We now manage the lock manually and track ownership via the
	// `held` flag so the deferred unlock only runs when we actually
	// own the lock.
	var zeroValue K
	ctx = xsync.WithEnableDeadlock(ctx, false)
	ctxAllowUnlock := xsync.WithAllowUnlockNotLocked(ctx, true)
	if !r.KernelLocker.ManualLock(ctx) {
		return ctx.Err()
	}
	held := true
	defer func() {
		if held {
			r.KernelLocker.ManualUnlock(ctxAllowUnlock)
		}
	}()
	for {
		k, err := r.getKernel(ctx)
		if err != nil {
			logger.Debugf(ctx, "unset kernel: getKernel error %v", err)
			r.Kernel = zeroValue
			r.KernelIsSet = false
			// ErrKernelNotSet is the transient signal from
			// getKernel's non-blocking barrier probe: the kernel
			// hasn't been opened yet (Unpause spawn is in flight or
			// scheduled). Treat as RETRY rather than fatal — block
			// on either the barrier being closed (kernel ready) or
			// the daemon ctx being cancelled, then loop back to
			// re-attempt getKernel. Without this, FromKernel
			// processor's Generate goroutine sees a synchronous
			// "kernel is not set" the moment getKernel races the
			// Unpause spawn, exits with that error, and the entire
			// InputChain.Serve unwinds before any frame can flow.
			// Symptom of the unwind: pipelines counters all zero, no
			// goroutine in CGO/syscall, chain.Serve "input node
			// serving started" → "ended" within the same second.
			if errors.As(err, &ErrKernelNotSet{}) {
				// Wait for either the barrier to be opened
				// (kernel ready) or for shutdown signals, then
				// continue the retry loop.
				r.KernelLocker.ManualUnlock(ctxAllowUnlock)
				held = false
				select {
				case <-*r.KernelOpenBarrier.Load():
					// barrier closed — kernel about to be ready
				case <-r.ClosureSignaler.CloseChan():
					return io.EOF
				case <-ctx.Done():
					return ctx.Err()
				}
				if !r.KernelLocker.ManualLock(ctx) {
					return ctx.Err()
				}
				held = true
				continue
			}
			return fmt.Errorf("unable to get kernel: %w", err)
		}

		// Release the lock for the long-running callback.
		r.KernelLocker.ManualUnlock(ctxAllowUnlock)
		held = false
		err = callback(k)
		if !r.KernelLocker.ManualLock(ctx) {
			// ctx is done — we never re-acquired, so do not unlock
			// in the deferred cleanup. held is already false.
			return ctx.Err()
		}
		held = true

		if err == nil {
			return nil
		}
		if r.OnError != nil {
			// Release KernelLocker around OnError so that concurrent
			// control ops (Pause / Unpause / AddInput) can acquire
			// it. OnError sleeps RetryInterval; holding the lock
			// across that sleep starves callers. See openKernelIfNeeded
			// for the matching reasoning. The held flag must follow
			// the lock state so the deferred unlock (above) does not
			// double-unlock when withKernelLockerReleased returns.
			held = false
			r.withKernelLockerReleased(ctxAllowUnlock, func() {
				err = r.OnError(ctx, k, err)
			})
			held = true
			switch {
			case err == nil:
				return nil
			case errors.As(err, &ErrRetry{}):
			default:
				logger.Debugf(ctx, "unset kernel: OnError error: %v", err)
				r.Kernel = zeroValue
				r.KernelIsSet = false
				r.KernelError = err
				r.Close(ctx)
				return err
			}
		}

		logger.Debugf(ctx, "unset kernel: callback error: %v", err)
		r.Kernel = zeroValue
		r.KernelIsSet = false
	}
}

func (r *Retryable[K]) getKernel(
	ctx context.Context,
) (K, error) {
	var zeroValue K
	if r.KernelError != nil {
		return zeroValue, r.KernelError
	}

	// Non-blocking barrier probe: getKernel is reached from the
	// synchronous SendInput/Generate→retry path (FromKernel processor
	// goroutine pulls frames). If the barrier is paused (kernel not yet
	// unpaused), we MUST NOT call openKernelIfNeeded — its select on
	// `<-ctx.Done()` would inherit the FromKernel goroutine's ctx, and
	// when that ctx is cancelled (chain teardown, RPC-scoped subtree)
	// it permanently sets KernelError = context.Canceled and wedges
	// the chain. The Pause/Unpause/Close paths handle barrier-wait
	// themselves on detached ctxs (see NewRetryable spawn @ line 75
	// and Unpause spawn @ line 511 — both wrapped with
	// xcontext.DetachDone). The synchronous getKernel path was the
	// missing third call site that the 0630171 fix didn't cover.
	//
	// When paused, return ErrKernelNotSet so the retry loop stops
	// cleanly — exactly the behavior already documented in the
	// pre-fix comment below; the implementation just didn't match
	// the intent.
	select {
	case <-*r.KernelOpenBarrier.Load():
		// barrier is open (closed channel) — proceed to open the
		// kernel synchronously. openKernelIfNeeded's barrier-wait
		// select returns immediately on the same closed-channel
		// receive, so this is bounded.
	default:
		// barrier is paused — defer the open to the Unpause spawn
		// (which wraps with xcontext.DetachDone). Surface
		// ErrKernelNotSet so the retry loop in retry() stops cleanly
		// and re-tries on the next round.
		return zeroValue, ErrKernelNotSet{}
	}

	r.openKernelIfNeeded(ctx)
	if !r.KernelIsSet && r.KernelError == nil {
		// openKernelIfNeeded deferred the open (barrier is paused).
		// We cannot return a zero-value kernel: callers will
		// dereference it. Surface a sentinel error instead so the
		// retry loop stops cleanly.
		return zeroValue, ErrKernelNotSet{}
	}
	return r.Kernel, r.KernelError
}

func (r *Retryable[K]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return r.retry(ctx, func(k K) (_err error) {
		defer func() {
			r := recover()
			if r != nil {
				_err = fmt.Errorf("panic in SendInput: %v:\n%s", r, debug.Stack())
			}
		}()
		return k.SendInput(ctx, input, outputCh)
	})
}

func (r *Retryable[K]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(r)
}

func (r *Retryable[K]) String() string {
	ctx := xsync.WithEnableDeadlock(context.Background(), false)
	// Use non-blocking lock to avoid deadlock when openKernelIfNeeded
	// holds KernelLocker while blocked on KernelOpenBarrier — any
	// goroutine calling String() (e.g. from debug logging in
	// AddPushTo) would block forever on the same lock.
	if !r.KernelLocker.ManualTryLock(ctx) {
		return "Retry(<locked>)"
	}
	snap := kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	r.KernelLocker.ManualUnlock(ctx)
	if !snap.isSet {
		return fmt.Sprintf("Retry(%T:<not set>)", snap.kernel)
	}
	return fmt.Sprintf("Retry(%T:%s)", snap.kernel, snap.kernel)
}

func (r *Retryable[K]) Unpause(ctx context.Context) (_err error) {
	select {
	case <-r.lifecycleCtx.Done():
		return nil
	default:
	}
	r.unpauseKernelOpening(ctx)
	// Detach the spawned goroutine's ctx from the caller's ctx. Reasoning:
	// the openKernelIfNeeded goroutine waits on r.KernelOpenBarrier, which is
	// flipped by Unpause/Pause cycles tracked through the Retryable's own
	// ClosureSignaler — the goroutine already exits cleanly via that signaler
	// when r.Close runs (line 182, "<-r.ClosureSignaler.CloseChan()").
	//
	// Without the detach, the goroutine inherits the caller's ctx — and
	// when Unpause is invoked from a request-scoped ctx (most notably the
	// gRPC per-call ctx in ffstream's chainPreExisted hot-reload branch in
	// AddInput), gRPC cancels that ctx the moment the RPC returns. The
	// goroutine then takes the <-ctx.Done() branch (line 175), sets
	// KernelError = context.Canceled, and the chain is permanently wedged:
	// every future openKernelIfNeeded short-circuits at line 135's
	// "if r.KernelIsSet || r.KernelError != nil { return }" guard, and no
	// SwitchOutputByProps can drive frames through the wedged kernel.
	//
	// The detach pattern matches the established convention in this
	// codebase for goroutines that outlive a request handler — e.g.
	// avpipeline/serve.go:110, router/router.go:292+297,
	// preset/streammux/output.go:242, and the documented rationale in
	// router/route_forwarding.go:97 ("xcontext.DetachDone or
	// context.WithoutCancel ... so that disconnect of the originating
	// request does not cancel the forwarder").
	observability.Go(r.lifecycleCtx, func(ctx context.Context) {
		r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
			r.openKernelIfNeeded(ctx)
		})
	})
	return nil
}

func (r *Retryable[K]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	r.ClosureSignaler.Close(ctx)
	if r.lifecycleCancel != nil {
		r.lifecycleCancel()
	}
	r.pauseKernelOpening(xcontext.DetachDone(ctx))
	observability.Go(xcontext.DetachDone(ctx), func(ctx context.Context) {
		err := r.Pause(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to stop the retry kernel: %v", err)
		}
	})
	return nil
}

func (r *Retryable[K]) IsPaused(ctx context.Context) bool {
	select {
	case <-*r.KernelOpenBarrier.Load():
		return false
	default:
		return true
	}
}

// IsKernelOpen reports whether the underlying kernel has been
// successfully opened by the Factory and not yet closed. It differs
// from !IsPaused (which reflects the operator's intent — barrier flipped
// to "should-be-open") by reflecting the actual KernelIsSet state.
//
// Use this when a caller needs to distinguish "open in flight" from
// "open and serving" — e.g. ffstream's chainPreExisted Pause+Unpause
// hot-reload kick at AddInput time, which must NOT fire while an open
// is in flight (closing a freshly-opened camera2 NDK session before
// it has stabilised triggers self-eviction in the camera service).
func (r *Retryable[K]) IsKernelOpen(ctx context.Context) bool {
	ctx = xsync.WithEnableDeadlock(ctx, false)
	if !r.KernelLocker.ManualLock(ctx) {
		return false
	}
	defer r.KernelLocker.ManualUnlock(ctx)
	return r.KernelIsSet && r.KernelError == nil
}

func (r *Retryable[K]) Pause(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Stop()")
	defer func() { logger.Debugf(ctx, "/Stop(): %v", _err) }()
	// Disable xsync deadlock detection for the lock acquire: every
	// other Retryable method that takes KernelLocker (Close, Unpause,
	// retry, openKernelIfNeeded, the format-context queries,
	// WithNetworkConn, ...) does the same, because the
	// KernelLocker → InputChainsLocker order spans across goroutines
	// (the xsync detector cannot model cross-goroutine hand-offs and
	// produces false positives). Pause was the lone outlier.
	ctx = xsync.WithEnableDeadlock(ctx, false)
	return xsync.DoA1R1(ctx, &r.KernelLocker, r.pauseLocked, ctx)
}

func (r *Retryable[K]) pauseLocked(ctx context.Context) error {
	if !r.KernelIsSet {
		// The kernel has not been opened yet, so there is nothing to
		// close. Flip the barrier so that openKernelIfNeeded does not
		// start opening a fresh kernel, and so that any concurrent
		// open that already has a kernel will close it upon seeing
		// the paused barrier. Without this, the caller's pause would
		// silently be lost and any in-flight open would proceed.
		logger.Debugf(ctx, "kernel is not set, flipping barrier to paused")
		r.pauseKernelOpening(ctx)
		return nil
	}
	err := r.Kernel.Close(ctx)
	if err != nil {
		return fmt.Errorf("unable to close kernel: %w", err)
	}
	logger.Debugf(ctx, "unset kernel")
	r.KernelIsSet = false
	r.pauseKernelOpening(ctx)
	return nil
}

func (r *Retryable[K]) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return r.retry(ctx, func(k K) error {
		return k.Generate(ctx, outputCh)
	})
}

func (r *Retryable[K]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	// Use non-blocking lock to avoid stalling callers when the init
	// goroutine holds KernelLocker during the factory retry loop
	// (e.g., output destination not yet reachable). Format context
	// queries are read-only — returning nil is safe when the kernel
	// is not yet ready.
	ctx = xsync.WithEnableDeadlock(ctx, false)
	if !r.KernelLocker.ManualTryLock(ctx) {
		logger.Debugf(ctx, "WithOutputFormatContext: kernel lock busy, calling callback with nil")
		callback(nil)
		return
	}
	snap := kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	r.KernelLocker.ManualUnlock(ctx)

	if !snap.isSet {
		logger.Debugf(ctx, "WithOutputFormatContext: kernel not set, calling callback with nil")
		callback(nil)
		return
	}
	pktSrc, ok := Abstract(snap.kernel).(packet.Source)
	if !ok {
		return
	}
	pktSrc.WithOutputFormatContext(ctx, callback)
}

func (r *Retryable[K]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	// Use non-blocking lock to avoid stalling callers when the init
	// goroutine holds KernelLocker during the factory retry loop
	// (e.g., output destination not yet reachable). Format context
	// queries are read-only — returning nil is safe when the kernel
	// is not yet ready.
	ctx = xsync.WithEnableDeadlock(ctx, false)
	if !r.KernelLocker.ManualTryLock(ctx) {
		logger.Debugf(ctx, "WithInputFormatContext: kernel lock busy, calling callback with nil")
		callback(nil)
		return
	}
	snap := kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	r.KernelLocker.ManualUnlock(ctx)

	if !snap.isSet {
		logger.Debugf(ctx, "WithInputFormatContext: kernel not set, calling callback with nil")
		callback(nil)
		return
	}
	pktSink, ok := Abstract(snap.kernel).(packet.Sink)
	if !ok {
		return
	}
	pktSink.WithInputFormatContext(ctx, callback)
}

func (r *Retryable[K]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return r.retry(ctx, func(k K) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		return pktSink.NotifyAboutPacketSource(ctx, source)
	})
}

var _ WithNetworkConner = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) WithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	return xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() error {
		if !r.KernelIsSet {
			return ErrKernelNotSet{}
		}
		k, ok := any(r.Kernel).(types.WithNetworkConner)
		if !ok {
			return ErrNotImplemented{
				Err: fmt.Errorf("kernel %T does not implement WithNetworkConner", r.Kernel),
			}
		}
		return k.WithNetworkConn(ctx, callback)
	})
}

var _ WithRawNetworkConner = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) WithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	return xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() error {
		if !r.KernelIsSet {
			return ErrKernelNotSet{}
		}
		k, ok := any(r.Kernel).(types.WithRawNetworkConner)
		if !ok {
			return ErrNotImplemented{
				Err: fmt.Errorf("kernel %T does not implement WithRawNetworkConner", r.Kernel),
			}
		}
		return k.WithRawNetworkConn(ctx, callback)
	})
}

var _ GetInternalQueueSizer = (*Retryable[Abstract])(nil)

type kernelSnapshot[K Abstract] struct {
	kernel K
	isSet  bool
}

func (r *Retryable[K]) openAttemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	openCtx, cancel := context.WithCancel(ctx)
	if r.lifecycleCtx == nil {
		return openCtx, cancel
	}
	stop := context.AfterFunc(r.lifecycleCtx, cancel)
	return openCtx, func() {
		if stop() {
			cancel()
			return
		}
		cancel()
	}
}

func (r *Retryable[K]) openAttemptStopped(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-r.ClosureSignaler.CloseChan():
		return io.EOF
	default:
		return nil
	}
}

func (r *Retryable[K]) discardOpenedKernel(ctx context.Context, k K, reason error) {
	logger.Debugf(ctx, "discarding opened kernel after open attempt stopped: %v", reason)
	r.closeOpenedKernel(ctx, k, "discarded kernel")
	if r.KernelError == nil {
		r.KernelError = reason
	}
}

func (r *Retryable[K]) closeOpenedKernel(ctx context.Context, k K, description string) {
	if any(k) != nil {
		cleanupCtx := xcontext.DetachDone(ctx)
		if closeErr := k.Close(cleanupCtx); closeErr != nil {
			logger.Errorf(ctx, "unable to close %s: %v", description, closeErr)
		}
	}
}

// withKernelLockerReleased runs fn while r.KernelLocker is temporarily
// released. The caller MUST own the lock on entry and the helper
// guarantees ownership on return — re-acquired with context.Background()
// so a cancelled caller ctx still leaves the lock in the expected state
// for the deferred unlocks higher up the stack (NewRetryable startup
// goroutine, retry()'s held-flag bookkeeping, Unpause.func1).
//
// The re-lock context is tagged with
// xsync.WithEnableDeadlock(context.Background(), false) so it matches
// the outer-scope deadlock-disabled tagging used by every Retryable
// top-level method that releases-and-reacquires KernelLocker (retry,
// openKernelIfNeeded). Re-locking with a bare Background would
// re-enable xsync's deadlock detector inside an outer-disabled scope
// and surface false positives across the already-known
// KernelLocker → InputChainsLocker hand-off.
//
// unlockCtx is passed verbatim to ManualUnlock; call sites that pass
// the AllowUnlockNotLocked-tagged ctx (retry() inner unlock) do so for
// the same reason as the inline pattern they replace.
//
// Why not use context.Background() unconditionally for the unlock too:
// the openKernelIfNeeded OnError site passes the caller's ctx so the
// xsync deadlock-detector instrumentation reports the original
// goroutine. Mixing background-ctx unlock with caller-ctx lock would
// hide deadlocks instead of surfacing them.
//
// SAFETY: any ctx-cancelled or panicking fn would otherwise strand the
// lock unheld (the caller's deferred unlock only sees `held=true`).
// We do NOT recover panics here — the historical bare ManualLock /
// ManualUnlock dance had the same property and call sites' OnError
// callbacks are expected not to panic. Adding a recover would mask
// real bugs.
func (r *Retryable[K]) withKernelLockerReleased(unlockCtx context.Context, fn func()) {
	r.KernelLocker.ManualUnlock(unlockCtx)
	defer func() {
		_ = r.KernelLocker.ManualLock(xsync.WithEnableDeadlock(context.Background(), false))
	}()
	fn()
}

func (r *Retryable[K]) GetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	snap := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() kernelSnapshot[K] {
		return kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	})
	if !snap.isSet {
		logger.Debugf(ctx, "GetInternalQueueSize: kernel is not set")
		return nil
	}
	queuer, ok := any(snap.kernel).(GetInternalQueueSizer)
	if !ok {
		return nil
	}
	return queuer.GetInternalQueueSize(ctx)
}

type ErrRetry struct {
	Err error
}

func (e ErrRetry) Error() string {
	return fmt.Sprintf("%s [please retry]", e.Err)
}

var _ types.GetOldestDTSInTheQueuer = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) GetOldestDTSInTheQueue(
	ctx context.Context,
) (time.Duration, error) {
	snap := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() kernelSnapshot[K] {
		return kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	})
	if !snap.isSet {
		return 0, ErrKernelNotSet{}
	}

	queuer, ok := any(snap.kernel).(types.GetOldestDTSInTheQueuer)
	if !ok {
		return 0, ErrNotImplemented{}
	}

	return queuer.GetOldestDTSInTheQueue(ctx)
}

type ErrKernelNotSet struct{}

func (e ErrKernelNotSet) Error() string {
	return "kernel is not set"
}
