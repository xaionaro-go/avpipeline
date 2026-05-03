// retryable_pause_during_barrier_wait_test.go reproduces the deadlock that
// blocked InputWithFallback fallback switching when an empty paused chain's
// FromKernel.Generate loop wedged the per-chain Retryable on its
// KernelOpenBarrier wait while still holding KernelLocker. The Pause call
// issued by the InputSwitch (when transitioning across the empty chain)
// then queued behind that wait forever, leaving the procN counter stuck and
// blocking subsequent fallback switches with "another switch is in
// progress".
//
// The fix releases KernelLocker around the barrier wait inside
// openKernelIfNeeded, mirroring the earlier fix that released it around the
// OnError sleep. This test asserts that property: with the kernel paused
// and a Generate-driven openKernelIfNeeded blocked on the barrier,
// concurrent Pause / Close / Unpause must complete promptly.

package kernel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// TestRetryable_PauseDuringBarrierWaitDoesNotDeadlock starts a Retryable in
// the StartOnInit=true config (so a Generate-style barrier wait is alive on
// the inner goroutine), pauses the kernel to close the barrier, then asserts
// that a subsequent Pause / Close round-trip completes within the deadline.
// Pre-fix the second Pause blocked on KernelLocker for the lifetime of the
// process; post-fix it returns immediately because openKernelIfNeeded
// releases the lock around its select.
func TestRetryable_PauseDuringBarrierWaitDoesNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var factoryCalls atomic.Int32
	r := NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			factoryCalls.Add(1)
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)

	// Wait until the startup goroutine has either opened the kernel or
	// is parked on the barrier; either way the inner select is reachable.
	require.Eventually(t, func() bool {
		return factoryCalls.Load() >= 1
	}, 2*time.Second, 5*time.Millisecond, "factory was never called")

	// Pause closes the kernel (if open) and flips the barrier to paused.
	require.NoError(t, r.Pause(ctx))

	// Drive a Generate cycle that will land in openKernelIfNeeded → select
	// (the barrier is paused). Pre-fix this goroutine holds KernelLocker
	// for the duration of the wait; post-fix it releases the lock around
	// the select, leaving Pause / Close free to acquire.
	outCh := make(chan packetorframe.OutputUnion, 1)
	generateDone := make(chan struct{})
	generateCtx, generateCancel := context.WithCancel(ctx)
	go func() {
		defer close(generateDone)
		_ = r.Generate(generateCtx, outCh)
	}()

	// Give the Generate goroutine a moment to enter the select. Under load
	// 50ms is plenty; we cross-check below by asserting the control-op
	// completion deadline, which is the actual property under test.
	time.Sleep(50 * time.Millisecond)

	// The control operations must each complete within a tight deadline.
	// 5s is overkill for a healthy lock release (sub-ms in practice) but
	// short enough to fail loudly when the deadlock regresses.
	pauseDone := make(chan error, 1)
	go func() { pauseDone <- r.Pause(ctx) }()
	select {
	case err := <-pauseDone:
		require.NoError(t, err, "Pause during barrier wait must not error")
	case <-time.After(5 * time.Second):
		t.Fatal("Pause deadlocked behind openKernelIfNeeded barrier wait — KernelLocker was held across the select")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- r.Close(ctx) }()
	select {
	case err := <-closeDone:
		require.NoError(t, err, "Close during barrier wait must not error")
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked behind openKernelIfNeeded barrier wait — KernelLocker was held across the select")
	}

	// Tear down the Generate goroutine so the test exits cleanly.
	generateCancel()
	select {
	case <-generateDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Generate goroutine did not exit after ctx cancel")
	}
}
