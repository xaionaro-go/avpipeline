// retryable_open_kernel_lock_release_test.go: openKernelIfNeeded must
// release KernelLocker around Config.OnKernelOpen. Otherwise any
// OnKernelOpen callback that transitively blocks on a downstream lock
// (e.g. resetDownstreamKernels → Decoder.ResetHard →
// codec.Decoder.locker held by a wedged h264_mediacodec
// avcodec_send_packet) wedges the entire Retryable — every
// Pause/Unpause/Generate queues behind KernelLocker forever.
//
// This file asserts the property: with OnKernelOpen blocking,
// concurrent Pause must complete within a tight deadline.

package kernel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRetryable_OnKernelOpenReleasesKernelLocker installs an
// OnKernelOpen callback that signals it has been entered, then blocks
// indefinitely on a release channel. The test then issues a concurrent
// Pause and asserts the Pause returns within a tight deadline (i.e.
// KernelLocker was released around the OnKernelOpen call).
//
// Pre-fix: OnKernelOpen was invoked under KernelLocker; the concurrent
// Pause queued behind it forever. Post-fix: KernelLocker is released
// around the OnKernelOpen call so Pause acquires promptly.
//
// Falsifier validation: dropping the withKernelLockerReleased wrapper
// in retryable.go (i.e. calling r.Config.OnKernelOpen directly under
// the lock) must make this test fail by hitting the 2s timeout.
func TestRetryable_OnKernelOpenReleasesKernelLocker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onOpenEntered := make(chan struct{})
	onOpenRelease := make(chan struct{})

	r := NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			close(onOpenEntered)
			<-onOpenRelease
			return nil
		}),
	)
	t.Cleanup(func() {
		// Release OnKernelOpen if not yet released so Close doesn't
		// block.
		select {
		case <-onOpenRelease:
		default:
			close(onOpenRelease)
		}
		_ = r.Close(context.Background())
	})

	select {
	case <-onOpenEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("OnKernelOpen was never entered (factory or barrier wedged)")
	}

	pauseDone := make(chan error, 1)
	go func() { pauseDone <- r.Pause(ctx) }()
	select {
	case err := <-pauseDone:
		require.NoError(t, err, "Pause during OnKernelOpen must not error")
	case <-time.After(2 * time.Second):
		t.Fatal("Pause deadlocked behind OnKernelOpen — KernelLocker held across Config.OnKernelOpen call")
	}

	close(onOpenRelease)

	// After OnKernelOpen returns and the open path's re-acquire runs,
	// the post-acquire barrier check must observe the paused barrier
	// (Pause flipped it while we were inside OnKernelOpen) and close
	// the kernel rather than installing it. The Retryable must remain
	// paused.
	require.Eventually(t, func() bool {
		return r.IsPaused(ctx)
	}, 2*time.Second, 5*time.Millisecond, "Retryable must remain paused after Pause-during-OnKernelOpen")
}

// closeCountingKernel is a kernel.Abstract whose Close increments an
// atomic counter on the supplied pointer. Used by
// TestRetryable_OnKernelOpen_NoLeakOnConcurrentOpen to observe the
// orphan-close path under -race without sharing mutable state via the
// non-atomic Dummy.CloseCallCount field.
type closeCountingKernel struct {
	Dummy
	closeCount *atomic.Int32
}

func (c *closeCountingKernel) Close(ctx context.Context) error {
	c.closeCount.Add(1)
	return c.Dummy.Close(ctx)
}

// TestRetryable_OnKernelOpen_NoLeakOnConcurrentOpen verifies the
// orphan-kernel close path on the SAFETY arm: while one
// openKernelIfNeeded call is in OnKernelOpen with the lock released, a
// concurrent open path could install a different kernel. The fix
// re-checks KernelIsSet on re-acquire and closes the orphan k via
// k.Close(ctx).
//
// Direct reproduction of the dual-opener race is timing-sensitive
// (both openers must reach the OnKernelOpen release window). Instead
// we drive the equivalent observable: have OnKernelOpen itself plant
// a different kernel on the Retryable (simulating a concurrent winner
// that took the lock during the release window), then assert that the
// factory-returned kernel gets Close()-d on the re-acquire path.
//
// Pre-fix: no orphan-close logic — the locally-allocated kernel `k`
// would be discarded silently and Close never called → CGo resources
// leak. Post-fix: k.Close is invoked, observable via the atomic
// counter on closeCountingKernel.
func TestRetryable_OnKernelOpen_NoLeakOnConcurrentOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factoryCloseCount := &atomic.Int32{}
	rReady := make(chan *Retryable[Abstract], 1)

	r := NewRetryable[Abstract](
		ctx,
		func(ctx context.Context) (Abstract, error) {
			return &closeCountingKernel{closeCount: factoryCloseCount}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](true),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			// Plant a different kernel on r while our KernelLocker is
			// released. We must take the lock briefly to do so safely.
			// This simulates the concurrent winner described in the
			// SAFETY comment in retryable.go::openKernelIfNeeded.
			//
			// Wait on rReady so we don't race the test's `r := …`
			// assignment — the startup goroutine inside NewRetryable
			// can enter OnKernelOpen before NewRetryable returns.
			var rr *Retryable[Abstract]
			select {
			case rr = <-rReady:
				// re-publish so any further re-entry sees it too
				select {
				case rReady <- rr:
				default:
				}
			case <-ctx.Done():
				return ctx.Err()
			}
			rr.KernelLocker.Do(ctx, func() {
				if rr.KernelIsSet {
					return
				}
				rr.Kernel = &Dummy{}
				rr.KernelIsSet = true
			})
			return nil
		}),
	)
	rReady <- r
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	// Wait for the orphan-close path to fire. Pre-fix this counter
	// stays at 0; post-fix it goes to ≥ 1.
	require.Eventually(t, func() bool {
		return factoryCloseCount.Load() >= 1
	}, 2*time.Second, 5*time.Millisecond,
		"orphan kernel from factory must be Close()-d when concurrent open won the race")
}
