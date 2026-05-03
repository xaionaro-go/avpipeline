// retryable_unpause_caller_ctx_cancel_test.go reproduces the wedge where
// Retryable.Unpause spawned its openKernelIfNeeded goroutine with the
// caller's ctx, so a request-scoped caller (e.g. a gRPC RPC handler whose
// per-call ctx is cancelled the moment the RPC returns) caused the
// goroutine to fall into the <-ctx.Done() branch of the barrier-wait
// select before the kernel was opened, set KernelError = context.Canceled,
// and permanently wedge the chain.
//
// The fix detaches the goroutine's ctx from the caller's ctx via
// xcontext.DetachDone — the Retryable manages its own lifecycle through
// ClosureSignaler, so cancellation of the caller must not propagate.
//
// Concrete production trace this test pins down: ffstream-camera daemon's
// chainPreExisted hot-reload branch in AddInput called chain.Unpause(ctx)
// with the gRPC per-call ctx; gRPC cancelled it on RPC return; the
// goroutine logged "retryable.go:178 unable to open the kernel, because
// we are finishing: context canceled" and the camera chain never
// recovered, so SwitchOutputByProps never delivered frames to the output.
//
// Falsifier intent (dual-sided): we assert both (a) the kernel DOES open
// (factory is called, KernelIsSet flips true) and (b) KernelError
// remains nil. Reverting Unpause to pass the caller's ctx (drop the
// xcontext.DetachDone call) flips both — factory is never called and
// KernelError observes context.Canceled.

package kernel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRetryable_UnpauseCallerCtxCancelDoesNotWedgeKernel emulates a
// request-scoped caller (per-RPC context) issuing Unpause, then having
// its ctx cancelled the moment the call returns — exactly as gRPC does
// to per-RPC contexts. The Retryable goroutine must NOT observe the
// cancellation: KernelError must remain nil and the kernel must open
// once the factory becomes invocable.
func TestRetryable_UnpauseCallerCtxCancelDoesNotWedgeKernel(t *testing.T) {
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	var factoryCalls atomic.Int32
	r := NewRetryable[Abstract](
		daemonCtx,
		func(ctx context.Context) (Abstract, error) {
			factoryCalls.Add(1)
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)

	// Pause is the initial state given StartOnInit=false; the factory
	// has not been called yet.
	require.Equal(t, int32(0), factoryCalls.Load(),
		"factory must not be called before Unpause")
	require.True(t, r.IsPaused(daemonCtx))

	// Simulate a request-scoped caller — its ctx is cancelled the
	// moment the call returns, mirroring gRPC's per-RPC ctx lifecycle.
	rpcCtx, rpcCancel := context.WithCancel(daemonCtx)
	require.NoError(t, r.Unpause(rpcCtx))
	rpcCancel()

	// Pre-fix: the goroutine takes the <-ctx.Done() branch in
	// openKernelIfNeeded, sets KernelError = context.Canceled, and
	// never invokes the factory.
	// Post-fix: the goroutine ignores rpcCtx cancellation
	// (xcontext.DetachDone severs the cancel chain) and proceeds to
	// open the kernel once the barrier is closed.
	require.Eventually(t, func() bool {
		return factoryCalls.Load() >= 1
	}, 2*time.Second, 5*time.Millisecond,
		"kernel never opened — the Unpause goroutine inherited the cancelled rpcCtx and wedged on KernelError = context.Canceled")

	// Belt-and-suspenders dual-sided: KernelError must be nil
	// (the wedge marker is absent) AND KernelIsSet must be true (the
	// kernel actually opened). Asserting only one side leaves the
	// other unverified.
	r.KernelLocker.Do(daemonCtx, func() {
		require.NoError(t, r.KernelError,
			"KernelError must remain nil; non-nil indicates the rpcCtx-cancel wedge has regressed")
		require.True(t, r.KernelIsSet,
			"KernelIsSet must be true after Unpause + factory completion")
	})
}
