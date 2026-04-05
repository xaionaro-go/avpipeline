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
	PauseRequested    bool
}

func NewRetryable[K Abstract](
	ctx context.Context,
	factory func(context.Context) (K, error),
	onErrorFunc RetryableFuncOnError[K],
	opts ...RetryableOption[K],
) *Retryable[K] {
	r := &Retryable[K]{
		ClosureSignaler: closuresignaler.New(),
		Factory:         factory,
		OnError:         onErrorFunc,
		Config:          RetryableOptions[K](opts).Config(),
		KernelLocker:    make(xsync.CtxLocker, 1),
	}
	r.KernelOpenBarrier.Pointer = ptr(make(chan struct{}))
	if r.Config.StartOnInit {
		r.unpauseKernelOpening(ctx)
		if r.Config.OnInit != nil {
			r.Config.OnInit(ctx, r)
		}
		observability.Go(ctx, func(ctx context.Context) {
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

		// Check barrier without blocking: openKernelIfNeeded runs
		// holding KernelLocker, so blocking here would prevent any
		// concurrent Pause/Unpause from making progress and can
		// deadlock with a Pause that flipped the barrier between
		// Unpause closing it and this goroutine acquiring the lock.
		// If the barrier is currently paused, honour the pause and
		// return: the next Unpause will spawn a fresh goroutine.
		select {
		case <-ctx.Done():
			logger.Errorf(ctx, "unable to open the kernel, because we are finishing: %v", ctx.Err())
			if r.KernelError == nil {
				r.KernelError = ctx.Err()
			}
			return
		case <-r.ClosureSignaler.CloseChan():
			logger.Errorf(ctx, "unable to open the kernel, because the retryable is being closed")
			if r.KernelError == nil {
				r.KernelError = io.EOF
			}
			return
		case <-*r.KernelOpenBarrier.Load():
			// barrier is open (closed channel) — proceed.
		default:
			logger.Debugf(ctx, "openKernelIfNeeded: barrier is paused, deferring kernel open")
			return
		}

		k, err := r.Factory(ctx)
		logger.Debugf(ctx, "factory results: %p %v", k, err)
		if err == nil {
			if r.Config.OnKernelOpen != nil {
				err := r.Config.OnKernelOpen(ctx, k)
				if err != nil {
					r.KernelError = err
					r.Close(ctx)
					return
				}
			}
			logger.Debugf(ctx, "set kernel")
			r.Kernel = k
			r.KernelIsSet = true
			// If Pause was called while the kernel was being opened,
			// honour that pause intent now. Otherwise the caller's pause
			// would silently be lost.
			if r.PauseRequested {
				logger.Debugf(ctx, "PauseRequested is set, applying pause to the freshly opened kernel")
				var zeroValue K
				closeErr := r.Kernel.Close(ctx)
				if closeErr != nil {
					logger.Errorf(ctx, "unable to close kernel while applying deferred pause: %v", closeErr)
				}
				r.Kernel = zeroValue
				r.KernelIsSet = false
				r.PauseRequested = false
				r.pauseKernelOpening(ctx)
			}
			return
		}

		if r.OnError == nil {
			r.KernelError = err
			r.Close(ctx)
			return
		}

		err = r.OnError(ctx, k, err)
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
) error {
	var zeroValue K
	ctx = xsync.WithEnableDeadlock(ctx, false)
	return xsync.DoR1(xsync.WithAllowUnlockNotLocked(ctx, true), &r.KernelLocker, func() error {
		for {
			k, err := r.getKernel(ctx)
			if err != nil {
				logger.Debugf(ctx, "unset kernel: getKernel error %v", err)
				r.Kernel = zeroValue
				r.KernelIsSet = false
				return fmt.Errorf("unable to get kernel: %w", err)
			}

			isLocked := func() (isLocked bool) {
				defer func() {
					isLocked = r.KernelLocker.ManualLock(ctx)
				}()
				r.KernelLocker.ManualUnlock(ctx)
				err = callback(k)
				return
			}()
			if err == nil {
				return nil
			}
			if !isLocked {
				return ctx.Err()
			}
			if r.OnError != nil {
				err = r.OnError(ctx, k, err)
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
	})
}

func (r *Retryable[K]) getKernel(
	ctx context.Context,
) (K, error) {
	var zeroValue K
	if r.KernelError != nil {
		return zeroValue, r.KernelError
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
	ctx := context.Background()
	snap := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() kernelSnapshot[K] {
		return kernelSnapshot[K]{kernel: r.Kernel, isSet: r.KernelIsSet}
	})
	return fmt.Sprintf("Retry(%T:%s)", snap.kernel, snap.kernel)
}

func (r *Retryable[K]) Unpause(ctx context.Context) (_err error) {
	// Clear any deferred pause intent: the caller is explicitly asking
	// for the kernel to be opened, which overrides a prior Pause that
	// was recorded while the kernel was not yet set.
	r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
		r.PauseRequested = false
	})
	r.unpauseKernelOpening(ctx)
	observability.Go(ctx, func(ctx context.Context) {
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
	observability.Go(ctx, func(ctx context.Context) {
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

func (r *Retryable[K]) Pause(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Stop()")
	defer func() { logger.Debugf(ctx, "/Stop(): %v", _err) }()
	return xsync.DoA1R1(ctx, &r.KernelLocker, r.pauseLocked, ctx)
}

func (r *Retryable[K]) pauseLocked(ctx context.Context) error {
	if !r.KernelIsSet {
		// The kernel has not been opened yet, so there is nothing to
		// close. Record the pause intent so that openKernelIfNeeded
		// applies it as soon as the kernel is opened, and flip the
		// barrier so that openKernelIfNeeded does not start opening a
		// fresh kernel. Without this, the caller's pause would
		// silently be lost and any in-flight open would proceed.
		logger.Debugf(ctx, "kernel is not set, recording pause intent")
		r.PauseRequested = true
		r.pauseKernelOpening(ctx)
		return nil
	}
	err := r.Kernel.Close(ctx)
	if err != nil {
		return fmt.Errorf("unable to close kernel: %w", err)
	}
	logger.Debugf(ctx, "unset kernel")
	r.KernelIsSet = false
	r.PauseRequested = false
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
	r.retry(ctx, func(k K) error {
		pktSrc, ok := Abstract(k).(packet.Source)
		if !ok {
			return nil
		}
		pktSrc.WithOutputFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retryable[K]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k K) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		pktSink.WithInputFormatContext(ctx, callback)
		return nil
	})
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
