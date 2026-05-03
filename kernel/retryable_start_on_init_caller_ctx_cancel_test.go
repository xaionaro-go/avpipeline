// retryable_start_on_init_caller_ctx_cancel_test.go pins down the
// goroutine-survival contract for NewRetryable's StartOnInit=true
// branch (line 72 of retryable.go), which spawns an
// openKernelIfNeeded goroutine to perform the initial Factory call.
//
// The wedge this guards against: when NewRetryable is called from a
// request-scoped handler — most notably ffstream's
// senderFactory.newOutputWithRetry inside StreamMux.SwitchToOutputByProps
// invoked off the gRPC SwitchOutputByProps RPC ctx — gRPC cancels the
// per-call ctx the moment the RPC returns. Without xcontext.DetachDone
// at line 72, the spawned goroutine's openKernelIfNeeded sees
// <-ctx.Done() in its barrier-wait select before reaching the Factory
// call, sets r.KernelError = context.Canceled, and the freshly-
// created Retryable is permanently wedged: every subsequent
// openKernelIfNeeded short-circuits at the
// `if r.KernelIsSet || r.KernelError != nil { return }` guard.
//
// Determinism strategy: the OnInit option callback (RetryableOptionOnInit)
// is invoked synchronously inside NewRetryable BEFORE the goroutine is
// spawned. We use OnInit to cancel rpcCtx — guaranteeing rpcCtx is
// already dead by the time the goroutine is queued. Without DetachDone,
// the goroutine inherits the cancelled rpcCtx; with DetachDone, it
// inherits a context whose Done channel never fires.
//
// Falsifier: revert retryable.go's NewRetryable line ~72
// `detachedCtx := xcontext.DetachDone(ctx)` to use bare `ctx` directly
// — this test must fail (factoryEntered never closes within 2s
// because openKernelIfNeeded takes the <-ctx.Done() branch before
// invoking Factory).

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewRetryable_StartOnInit_SurvivesCallerCtxCancel(t *testing.T) {
	// Race amplification: the underlying race is the select in
	// openKernelIfNeeded between <-ctx.Done() and <-barrier; both
	// channels are ready under the bug, so Go's scheduler picks
	// randomly. With the DetachDone fix, <-ctx.Done() is the
	// background-Done channel (nil), so the select can ONLY pick the
	// barrier branch — deterministic. Without the fix, ~50% of runs
	// pick <-ctx.Done() and wedge. Iterating the construct N times
	// drives the no-failure probability to (0.5)^N — at iterations=20
	// the false-pass rate is ~1e-6, low enough for CI determinism
	// without depending on GOMAXPROCS=1 hacks.
	const iterations = 20
	for iter := 0; iter < iterations; iter++ {
		runRetryableStartOnInitSurvivalIter(t, iter)
		if t.Failed() {
			return
		}
	}
}

func runRetryableStartOnInitSurvivalIter(t *testing.T, iter int) {
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	// rpcCtx mimics gRPC's per-call ctx: cancelled the moment the
	// RPC handler returns. We arrange for cancellation to fire from
	// inside OnInit (which runs synchronously before the spawned
	// goroutine is queued), so the goroutine reliably observes a
	// dead caller-ctx pre-fix.
	rpcCtx, rpcCancel := context.WithCancel(daemonCtx)

	// factoryEntered is closed by the Factory the first time it's
	// invoked. Channel-close (not polling) is the witness that the
	// spawned goroutine survived rpcCtx cancellation and reached the
	// Factory call.
	factoryEntered := make(chan struct{})
	// kernelOpened is closed by the OnKernelOpen callback after
	// Factory completes successfully — used as the channel-signal
	// for the dual-sided KernelError-is-nil assertion (Factory ran
	// to completion AND no error path fired).
	kernelOpened := make(chan struct{})

	r := NewRetryable[Abstract](
		rpcCtx,
		func(ctx context.Context) (Abstract, error) {
			close(factoryEntered)
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
		RetryableOptionOnInit[Abstract](func(ctx context.Context, _ *Retryable[Abstract]) {
			// Synchronously cancel rpcCtx before the goroutine is
			// spawned. Pre-fix this means the spawned goroutine sees
			// rpcCtx.Done immediately on entering its select and
			// (~50% of the time) returns at line 189 without
			// invoking Factory. Iterating N times in the parent
			// drives that probability to ~1.
			rpcCancel()
		}),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			close(kernelOpened)
			return nil
		}),
	)
	require.NotNil(t, r)

	defer func() {
		_ = r.Close(context.Background())
	}()

	select {
	case <-factoryEntered:
		// Pass: Factory was reached even though rpcCtx was cancelled
		// before the goroutine was queued.
	case <-time.After(2 * time.Second):
		t.Fatalf("iter=%d: Factory was never invoked — the NewRetryable StartOnInit goroutine inherited the cancelled rpcCtx and took the <-ctx.Done() branch in openKernelIfNeeded's barrier-wait select before reaching the Factory call (xcontext.DetachDone fix at NewRetryable line 72 has regressed)", iter)
	}

	// Dual-sided via channel-signal: kernelOpened fires from
	// OnKernelOpen which runs after Factory returns successfully and
	// before r.KernelIsSet is set. Once we observe this signal, we
	// know openKernelIfNeeded reached the success path — meaning
	// KernelError was NOT set to ctx.Err() in any of the failure
	// branches at lines 192-194 / 199-201 / 264.
	select {
	case <-kernelOpened:
	case <-time.After(2 * time.Second):
		t.Fatalf("iter=%d: OnKernelOpen was never invoked — Factory may have returned but openKernelIfNeeded did not reach the success path (KernelError likely set to ctx.Err() by a regression)", iter)
	}

	// Belt-and-suspenders KernelError check.
	r.KernelLocker.Do(daemonCtx, func() {
		require.NoErrorf(t, r.KernelError, "iter=%d: KernelError must remain nil", iter)
		require.Truef(t, r.KernelIsSet, "iter=%d: KernelIsSet must be true", iter)
	})
}
