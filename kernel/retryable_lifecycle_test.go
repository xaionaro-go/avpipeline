package kernel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/xsync"
)

func TestRetryable_CloseCancelsBlockingFactoryAndPreventsLateKernelInstall(t *testing.T) {
	ctx := context.Background()
	factoryCtxCh := make(chan context.Context, 1)
	factoryDone := make(chan error, 1)
	closeCtxErrCh := make(chan error, 1)
	lateKernel := &Dummy{
		CloseFn: func(ctx context.Context) error {
			closeCtxErrCh <- ctx.Err()
			return nil
		},
	}

	r := NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			factoryCtxCh <- ctx
			<-ctx.Done()
			factoryDone <- ctx.Err()
			return lateKernel, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	select {
	case <-factoryCtxCh:
	case <-time.After(2 * time.Second):
		t.Fatal("factory was not started")
	}

	require.NoError(t, r.Close(ctx))

	select {
	case err := <-factoryDone:
		require.Error(t, err, "factory context should be canceled by Retryable.Close")
	case <-time.After(2 * time.Second):
		t.Fatal("Retryable.Close did not cancel the in-flight factory context")
	}

	require.Eventually(t, func() bool {
		return !r.IsKernelOpen(context.Background())
	}, 2*time.Second, 5*time.Millisecond, "factory result must not install a kernel after Close")

	kernelIsSet := xsync.DoR1(xsync.WithEnableDeadlock(context.Background(), false), &r.KernelLocker, func() bool {
		return r.KernelIsSet
	})
	require.False(t, kernelIsSet, "late factory result must not remain installed after Close")

	select {
	case closeCtxErr := <-closeCtxErrCh:
		require.NoError(t, closeCtxErr, "late factory result must be closed with a live cleanup context")
	case <-time.After(2 * time.Second):
		t.Fatal("late factory result was not closed")
	}
}

func TestRetryable_IsKernelOpenRespectsContextWhileFactoryHoldsLock(t *testing.T) {
	ctx := context.Background()
	factoryEntered := make(chan struct{})
	releaseFactory := make(chan struct{})
	var released atomic.Bool

	r := NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			close(factoryEntered)
			<-releaseFactory
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	t.Cleanup(func() {
		if released.CompareAndSwap(false, true) {
			close(releaseFactory)
		}
		_ = r.Close(context.Background())
	})

	select {
	case <-factoryEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("factory was not started")
	}

	checkCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		done <- r.IsKernelOpen(checkCtx)
	}()

	select {
	case isOpen := <-done:
		require.False(t, isOpen, "kernel should not report open while Factory is still blocked")
	case <-time.After(500 * time.Millisecond):
		if released.CompareAndSwap(false, true) {
			close(releaseFactory)
		}
		t.Fatal("IsKernelOpen blocked behind the factory instead of respecting its caller context")
	}
}

func TestRetryable_ConcurrentOnKernelOpenOrphanClosesWithLiveContext(t *testing.T) {
	ctx := context.Background()
	closeCtxErrCh := make(chan error, 1)

	var r *Retryable[Abstract]
	r = NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{
				CloseFn: func(ctx context.Context) error {
					closeCtxErrCh <- ctx.Err()
					return nil
				},
			}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			r.KernelError = context.Canceled
			return nil
		}),
	)
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	r.unpauseKernelOpening(context.Background())

	// The orphan branch runs after openAttemptStopped has already seen a
	// live openCtx. This test isolates that branch with a caller ctx that is
	// unusable for cleanup, matching kernels whose Close gates on ctx.Err().
	orphanCtx := errOnlyCanceledContext{Context: context.Background()}
	require.True(t, r.KernelLocker.ManualLock(orphanCtx))
	r.openKernelIfNeeded(orphanCtx)
	r.KernelLocker.ManualUnlock(orphanCtx)

	select {
	case closeCtxErr := <-closeCtxErrCh:
		require.NoError(t, closeCtxErr, "orphan kernel must be closed with a live cleanup context")
	case <-time.After(2 * time.Second):
		t.Fatal("orphan kernel was not closed")
	}
}

func TestRetryable_DeferredPauseClosesFreshKernelWithLiveContext(t *testing.T) {
	ctx := context.Background()
	closeCtxErrCh := make(chan error, 1)

	var r *Retryable[Abstract]
	r = NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{
				CloseFn: func(ctx context.Context) error {
					closeCtxErrCh <- ctx.Err()
					return nil
				},
			}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			r.pauseKernelOpening(context.Background())
			return nil
		}),
	)
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	r.unpauseKernelOpening(context.Background())

	openCtx := errOnlyCanceledContext{Context: context.Background()}
	require.True(t, r.KernelLocker.ManualLock(openCtx))
	r.openKernelIfNeeded(openCtx)
	require.False(t, r.KernelIsSet, "deferred pause must clear the freshly-opened kernel")
	r.KernelLocker.ManualUnlock(openCtx)

	select {
	case closeCtxErr := <-closeCtxErrCh:
		require.NoError(t, closeCtxErr, "deferred-pause cleanup must close with a live cleanup context")
	case <-time.After(2 * time.Second):
		t.Fatal("deferred-pause cleanup did not close the freshly-opened kernel")
	}
}

type errOnlyCanceledContext struct {
	context.Context
}

func (errOnlyCanceledContext) Done() <-chan struct{} {
	return nil
}

func (errOnlyCanceledContext) Err() error {
	return context.Canceled
}
