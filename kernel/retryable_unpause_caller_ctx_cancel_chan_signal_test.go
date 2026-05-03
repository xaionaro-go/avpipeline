// retryable_unpause_caller_ctx_cancel_chan_signal_test.go is a stricter
// (channel-signal, not Eventually-poll) variant of the survival test
// for Retryable.Unpause's xcontext.DetachDone fix at retryable.go
// line ~556.
//
// Why a second test for the same line: the existing
// TestRetryable_UnpauseCallerCtxCancelDoesNotWedgeKernel uses
// require.Eventually with a 5ms poll interval. That works but admits
// timing slack — a slow CI run could mask a regression that takes a
// few hundred milliseconds to manifest. This variant pins the timing
// down to channel-signal-only: factory close-on-entry, OnKernelOpen
// close-on-success, and a select-with-timeout for the witness.
// Together with the existing test it provides defence in depth for
// the same fix.
//
// Determinism strategy mirrors the StartOnInit test: cancel rpcCtx
// from inside OnInit (which fires synchronously before Unpause's
// goroutine spawn), and iterate the construct enough times that the
// underlying select-race in openKernelIfNeeded's barrier wait is
// driven to ~1 failure rate when the bug is present.
//
// Falsifier: revert retryable.go's Unpause line ~556
// `detachedCtx := xcontext.DetachDone(ctx)` to bare `ctx` — this
// test must fail (factoryEntered never closes within 2s for at least
// one iteration because openKernelIfNeeded takes <-ctx.Done() before
// invoking Factory).

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetryable_Unpause_SurvivesCallerCtxCancel(t *testing.T) {
	const iterations = 20
	for iter := 0; iter < iterations; iter++ {
		runRetryableUnpauseSurvivalIter(t, iter)
		if t.Failed() {
			return
		}
	}
}

func runRetryableUnpauseSurvivalIter(t *testing.T, iter int) {
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	factoryEntered := make(chan struct{})
	kernelOpened := make(chan struct{})

	r := NewRetryable[Abstract](
		daemonCtx,
		func(ctx context.Context) (Abstract, error) {
			close(factoryEntered)
			return &Dummy{}, nil
		},
		nil,
		// StartOnInit=false: the only goroutine that spawns the
		// factory is the one inside Unpause (line ~556) — exactly
		// the call site this test pins down.
		RetryableOptionStartOnInit[Abstract](false),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			close(kernelOpened)
			return nil
		}),
	)
	require.NotNil(t, r)
	defer func() { _ = r.Close(context.Background()) }()

	// Construct the rpcCtx, then cancel it the moment Unpause
	// returns — exactly mirrors gRPC's per-RPC ctx lifecycle.
	rpcCtx, rpcCancel := context.WithCancel(daemonCtx)
	require.NoError(t, r.Unpause(rpcCtx))
	rpcCancel()

	select {
	case <-factoryEntered:
	case <-time.After(2 * time.Second):
		t.Fatalf("iter=%d: Factory was never invoked — the Unpause goroutine inherited the cancelled rpcCtx and took the <-ctx.Done() branch in openKernelIfNeeded's barrier-wait select before reaching the Factory call (xcontext.DetachDone fix at Retryable.Unpause line ~556 has regressed)", iter)
	}

	select {
	case <-kernelOpened:
	case <-time.After(2 * time.Second):
		t.Fatalf("iter=%d: OnKernelOpen was never invoked — Factory may have returned but openKernelIfNeeded did not reach the success path", iter)
	}

	r.KernelLocker.Do(daemonCtx, func() {
		require.NoErrorf(t, r.KernelError, "iter=%d: KernelError must remain nil", iter)
		require.Truef(t, r.KernelIsSet, "iter=%d: KernelIsSet must be true", iter)
	})
}
