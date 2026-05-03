// input_chain_reset_timeout_test.go: each per-processor Reset call
// inside resetDownstreamKernels is wrapped in a context.WithTimeout
// boundary, so a wedged downstream Reset (e.g. Decoder.ResetHard
// blocked behind a hardware-codec-level silent-consume stall) does
// NOT indefinitely wedge the OnKernelOpen path. Instead the affected
// Reset is skipped with a Warn log, and the loop continues with the
// next downstream processor.
//
// Defense-in-depth: even if the structural lock-order fix that
// releases KernelLocker around OnKernelOpen regresses, the wedge can
// never last more than the configured timeout.

package inputwithfallback

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// blockingResetter is a Resetter whose Reset blocks until the context
// is cancelled. Used to drive the timeout branch deterministically.
type blockingResetter struct {
	calls atomic.Int32
}

func (b *blockingResetter) Reset(ctx context.Context) error {
	b.calls.Add(1)
	<-ctx.Done()
	// Match codec.Decoder.LockDo's cancellation-on-acquire behavior:
	// xsync.DoR1 returns the locked function's zero value (nil) when
	// ManualLock fails on ctx.Done. The runResetters helper detects
	// the timeout via the per-call context's Err, NOT via this
	// return value.
	return nil
}

// fastResetter is a Resetter whose Reset returns immediately. Used to
// assert the loop continues past a timeout.
type fastResetter struct {
	calls atomic.Int32
	err   error
}

func (f *fastResetter) Reset(ctx context.Context) error {
	f.calls.Add(1)
	return f.err
}

// TestRunResetters_TimeoutSkipsBlockedReset asserts that a Reset which
// blocks indefinitely is abandoned at the timeout, the loop continues,
// and runResetters returns nil (the timed-out entry is NOT reported
// as an error — it is logged as Warn and skipped).
//
// Falsifier: removing the context.WithTimeout wrapper (calling
// nr.r.Reset(ctx) directly) would make this test hang past the
// testTimeout deadline.
func TestRunResetters_TimeoutSkipsBlockedReset(t *testing.T) {
	const (
		resetTimeout = 75 * time.Millisecond
		testTimeout  = 2 * time.Second
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocker := &blockingResetter{}
	follower := &fastResetter{}

	resetters := []namedResetter{
		{"Blocker", blocker},
		{"Follower", follower},
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- runResetters(ctx, 99, resetTimeout, resetters)
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(testTimeout):
		t.Fatalf("runResetters did not honor per-call timeout; blocker still wedging after %s", testTimeout)
	}

	elapsed := time.Since(start)
	require.NoError(t, err, "timed-out entries are skipped (Warn-logged), not reported as errors")

	// Blocker entered Reset exactly once; Follower must have run too.
	require.Equal(t, int32(1), blocker.calls.Load(), "blocker Reset must be invoked once")
	require.Equal(t, int32(1), follower.calls.Load(), "follower Reset must run after blocker times out (loop continues)")

	// Total elapsed time must be ≈ resetTimeout (plus follower's near-
	// instant completion), not testTimeout. We allow a generous upper
	// bound to absorb scheduler jitter on loaded CI but reject anything
	// beyond 4× the expected.
	require.Less(t, elapsed, 4*resetTimeout,
		"runResetters elapsed %s exceeds 4× resetTimeout=%s — timeout boundary is leaking", elapsed, resetTimeout)
}

// TestRunResetters_NonTimeoutErrorPropagates confirms the Warn-vs-error
// distinction: a Reset that returns a real error (not a context-
// deadline outcome) must be accumulated as an error, not silently
// swallowed.
func TestRunResetters_NonTimeoutErrorPropagates(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("Reset failed for non-timeout reason")
	failer := &fastResetter{err: sentinel}
	resetters := []namedResetter{
		{"Failer", failer},
	}
	err := runResetters(ctx, 42, time.Second, resetters)
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel,
		"non-timeout error must be propagated — only context-deadline Reset outcomes are demoted to Warn")
}

// TestRunResetters_AllResettersRun_NoTimeouts is the dual-sided check:
// without timeouts firing, every Resetter must execute and the
// aggregate error must be nil.
func TestRunResetters_AllResettersRun_NoTimeouts(t *testing.T) {
	ctx := context.Background()
	a := &fastResetter{}
	b := &fastResetter{}
	c := &fastResetter{}
	resetters := []namedResetter{
		{"A", a}, {"B", b}, {"C", c},
	}
	err := runResetters(ctx, 0, time.Second, resetters)
	require.NoError(t, err)
	require.Equal(t, int32(1), a.calls.Load())
	require.Equal(t, int32(1), b.calls.Load())
	require.Equal(t, int32(1), c.calls.Load())
}

// TestRunResetters_OuterCtxCancelDoesNotMislabelAsTimeout pins the
// outer-ctx guard inside runResetters: when the CALLER's ctx is
// cancelled (not the per-call timeout), the entry must NOT be reported
// as a timeout-skip — it must just propagate the cancellation through
// the normal err path. Otherwise any caller cancellation would emit
// a misleading "reset timed out" Warn.
func TestRunResetters_OuterCtxCancelDoesNotMislabelAsTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Resetter that returns ctx.Err() once cancelled, like a real
	// xsync-backed Reset would.
	probe := &cancelObservingResetter{}
	resetters := []namedResetter{{"Probe", probe}}

	// Cancel the outer ctx FIRST; runResetters should see the outer
	// cancellation and not label the no-op as a per-call timeout.
	cancel()
	err := runResetters(ctx, 7, time.Second, resetters)
	// The probe returns ctx.Err() = context.Canceled, which is a real
	// error — runResetters propagates it as such.
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled,
		"outer-ctx cancellation must propagate as error, not be demoted to a timeout-skip Warn")
}

// cancelObservingResetter forwards ctx.Err() — emulating the behavior
// of a Reset that observes the cancellation and returns it.
type cancelObservingResetter struct{}

func (c *cancelObservingResetter) Reset(ctx context.Context) error {
	return ctx.Err()
}
