// eviction_recreate_retry_test.go pins the fix:
//
//   - evictDeadOutput must round-trip the OutputsMap entry through
//     Output.StorageKey(), not GetKey() — reconfigureEncoder fills the
//     EncoderFactory with both video AND audio axes, so GetKey() drifts
//     from the split key the entry was stored under.
//   - A periodic retry tick must re-fire the recreate hook for
//     SenderKeys whose backoff has elapsed without a fresh eviction
//     event. After the first eviction the dead Output is detached and
//     subsequent in-flight frames are dropped without producing node-
//     level errors; without the timer-driven retry, only attempt 1/N
//     ever fires.
//
// All three tests follow the dual-sided + falsifier-validated discipline
// required by testing-discipline.

package streammux

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

// TestRecreateEvictedOutput_SplitAVCompoundKey_DecomposesCorrectly is
// the dual-sided proof for the StorageKey detach fix.
//
// Setup mirrors the production wedge: SplitAV mode, a video-only
// Output whose EncoderFactory has been "reconfigured" so its
// VideoCodec AND AudioCodec are both populated (matching what
// reconfigureEncoder writes onto the factory). The Output was
// originally stored in OutputsMap under the SPLIT key
// (VideoCodec=av1, AudioCodec=""), but GetKey() now returns the
// COMPOUND key (VideoCodec=av1, AudioCodec=aac).
//
//   - GOOD-side: evictDeadOutput uses StorageKey() to detach the
//     OutputsMap entry, so the entry is removed cleanly. The
//     OutputSwitch is demoted to MinInt32 and recommit fires.
//   - BAD-side: keying the CompareAndDelete on GetKey() would silently
//     miss because GetKey() != the stored split key — the
//     OutputsMap[splitKey] entry would persist, leaving a stale
//     dead-output reference for the next eviction cycle to trip over.
//
// Falsification protocol: replace `output.StorageKey()` with
// `output.GetKey()` in evictDeadOutput's CompareAndDelete call —
// this test MUST fail at the OutputsMap.Load(splitKey) assertion
// because the stale entry would still be present.
func TestRecreateEvictedOutput_SplitAVCompoundKey_DecomposesCorrectly(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	// Original split key the OutputsMap was indexed by (matches what
	// getOrCreateOutputLocked stores in SplitAV mode for a video-only
	// output: getInputsForSenderKey decomposes the compound config
	// into a video-only SenderKey).
	splitKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	}

	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, splitKey)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(splitKey, dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// Simulate the post-reconfigureEncoder state: the EncoderFactory
	// now carries BOTH the video AND audio codec axes, so GetKey()
	// returns a COMPOUND key that differs from StorageKey().
	dead.TranscoderNode.Processor.Kernel.EncoderFactory.VideoCodec = codec.Name("av1")
	dead.TranscoderNode.Processor.Kernel.EncoderFactory.AudioCodec = codec.Name("aac")
	dead.TranscoderNode.Processor.Kernel.EncoderFactory.AudioSampleRate = 48000

	compoundKey := dead.GetKey()
	require.NotEqual(t, splitKey, compoundKey,
		"test setup: post-reconfigure GetKey() must drift from the StorageKey (compound vs split) — otherwise this test cannot expose Bug A")
	require.Equal(t, splitKey, dead.StorageKey(),
		"StorageKey must remain the original split key even after EncoderFactory mutation")

	// Block the recreate hook so it does NOT spin up a new Output —
	// this test asserts the eviction-side detach behaviour, not the
	// recreate-side behaviour. Returning a non-nil error keeps the
	// state at consecutiveFailures=1 + permanentlyFailed=false.
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		return errors.New("recreate suppressed for this test")
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: evictDeadOutput keyed on StorageKey() removes the
	// OutputsMap[splitKey] entry cleanly.
	_, okSplit := mux.OutputsMap.Load(splitKey)
	require.False(t, okSplit, "OutputsMap[splitKey] must be removed by evictDeadOutput keyed on StorageKey()")

	// BAD-side: the COMPOUND key was never stored, so Load(compoundKey)
	// is not the assertion we want — but we DO want to confirm that
	// the dead Output is no longer reachable under any key. Walk
	// OutputsMap to confirm zero entries point at `dead`.
	var staleEntries int
	mux.OutputsMap.Range(func(_ SenderKey, o *Output[struct{}]) bool {
		if o == dead {
			staleEntries++
		}
		return true
	})
	require.Equal(t, 0, staleEntries, "no OutputsMap entry may still point at the evicted dead output")

	// GOOD-side: OutputID-keyed map also cleared.
	_, okOutputs := mux.Outputs.Load(deadID)
	require.False(t, okOutputs, "OutputID-keyed Outputs map must also be cleared")

	// GOOD-side: OutputSwitch demoted to MinInt32 (Bug A's downstream
	// trigger for the recreate path that exercises Bug B).
	require.Equal(t, int32(math.MinInt32),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"OutputSwitch on the orphaned video input must be demoted to MinInt32")

	// GOOD-side: recreate state recorded under the SPLIT key, not the
	// compound key. The retry loop keys on this same SenderKey so the
	// lookup must agree.
	mux.evictionRecreateLocker.Lock()
	_, hasSplit := mux.lastEvictionRecreateState[splitKey]
	_, hasCompound := mux.lastEvictionRecreateState[compoundKey]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, hasSplit, "eviction-recreate state must be keyed by the SplitKey (StorageKey)")
	require.False(t, hasCompound, "eviction-recreate state must NOT be keyed by the post-reconfigure compound key")
}

// TestEvictionRecreate_PeriodicRetryFiresWithoutNewEviction is the
// dual-sided proof for the periodic retry tick.
//
// Without the periodic retry tick: after the first eviction's
// recreate fails, the in-flight frames hit a demoted-input drop path
// that does NOT produce node-level errors, so handleOutputNodeError
// → evictDeadOutput is never invoked again. consecutiveFailures
// stays at 1 forever. The retry loop fixes that by re-firing the
// recreate hook on a timer when the per-key backoff has elapsed.
//
//   - GOOD-side: a single eviction event followed by clock-advance
//     past InitialBackoff drives a SECOND recreate-hook invocation
//     via evictionRecreateRetryTick (no second on-eviction path).
//   - BAD-side: without the retry tick, calls would stay at 1.
//
// Falsification protocol: comment out `s.startEvictionRecreateRetryLoop`
// in StreamMux.Serve AND `tryRecreateForRetry`'s recreateEvictedOutputFunc
// invocation — this test (which calls the tick directly) MUST fail
// because no second hook fire would happen.
func TestEvictionRecreate_PeriodicRetryFiresWithoutNewEviction(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	// Tight backoff so the elapsed math is straightforward.
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        50 * time.Millisecond,
		MaxAttempts:       5,
		MaxAge:            time.Hour,
	}

	const deadID OutputID = 42
	// Non-empty VideoCodec is required: getInputsForSenderKey (used
	// by the retry tick to resolve which input owns the SenderKey)
	// only routes a key to InputVideoOnly when VideoCodec != "".
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	})

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("simulated persistent encoder fault")
	}

	// Setup: prime the eviction state via a real evictDeadOutput call
	// (attempt 1 fires).
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 1, calls, "attempt 1 fires from the on-eviction path")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"OutputSwitch must be demoted after the first eviction")

	// Tick BEFORE the backoff has elapsed: must NOT fire (gates on
	// SkipBackoff). The state stays at consecutiveFailures=1.
	now = now.Add(5 * time.Millisecond) // 5ms < 20ms (consecutiveFailures=1 backoff)
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 1, calls, "retry tick must not fire while backoff window is active")

	// Tick AFTER the backoff has elapsed: must fire WITHOUT any new
	// eviction event. This is the load-bearing assertion: in the
	// pre-fix world there would be no second hook invocation because
	// no on-eviction path runs.
	now = now.Add(20 * time.Millisecond) // total 25ms > 20ms backoff
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 2, calls, "retry tick must fire attempt 2 once backoff has elapsed")

	// State updated: consecutiveFailures bumped to 2, lastFailureTime
	// refreshed to now. Without the bump, every subsequent retry tick
	// would also fire (the gate would not advance).
	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.StorageKey()]
	mux.evictionRecreateLocker.Unlock()
	require.Equal(t, 2, state.consecutiveFailures, "retry-driven attempt must bump consecutiveFailures")
	require.Equal(t, now, state.lastFailureTime, "retry-driven attempt must refresh lastFailureTime")

	// BAD-side: another tick immediately after must NOT fire (the
	// backoff for consecutiveFailures=2 is 40ms, capped at 50ms).
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 2, calls, "consecutive retry ticks within the new (escalated) backoff must not fire")
}

// TestEvictionRecreate_PeriodicRetryStopsAfterRecovery is the dual-
// sided proof that the retry tick is gated on the still-demoted
// signal: once OutputSwitch advances off MinInt32 (recreate succeeded
// or external action recovered the input), subsequent ticks must NOT
// re-fire.
//
//   - GOOD-side: with the still-demoted gate, a tick after the input
//     has recovered does NOT invoke the recreate hook.
//   - BAD-side: without the gate, the retry would burn CPU re-firing
//     forever (until MaxAge cleans the state entry).
//
// Falsification protocol: remove the `demotedInput == nil` early
// return in tryRecreateForRetry — this test MUST fail because the
// post-recovery tick would still call the hook.
func TestEvictionRecreate_PeriodicRetryStopsAfterRecovery(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        50 * time.Millisecond,
		MaxAttempts:       5,
		MaxAge:            time.Hour,
	}

	const deadID OutputID = 42
	const recoveredID OutputID = 99
	// Non-empty VideoCodec — see TestEvictionRecreate_PeriodicRetryFiresWithoutNewEviction
	// for the getInputsForSenderKey routing constraint.
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	})

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("simulated fault")
	}

	// Drive attempt 1 from the on-eviction path.
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 1, calls, "attempt 1 fires from the on-eviction path")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"input demoted post-eviction")

	// Simulate recovery: the recreate path (or some other path) has
	// switched the input onto a fresh Output that is no longer
	// MinInt32. The state entry stays in the map (cleaned by MaxAge
	// on the next eviction or sliding-window reset).
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(recoveredID))

	// Advance past the backoff and tick. The still-demoted gate must
	// suppress the recreate hook.
	now = now.Add(50 * time.Millisecond) // > 20ms backoff for n=1
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 1, calls, "retry tick must NOT fire when input has recovered (OutputSwitch != MinInt32)")

	// State must NOT have advanced — no second hook fire means no
	// state mutation. consecutiveFailures stays at 1.
	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.StorageKey()]
	mux.evictionRecreateLocker.Unlock()
	require.Equal(t, 1, state.consecutiveFailures, "skipped retry must not bump consecutiveFailures")
	require.False(t, state.permanentlyFailed, "skipped retry must not latch permanentlyFailed")

	// BAD-side: subsequent ticks at any time interval must also stay
	// at 1 call as long as the input is recovered.
	now = now.Add(time.Second) // far past any backoff
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 1, calls, "post-recovery ticks must remain suppressed regardless of elapsed time (until MaxAge reset)")
}

// TestEvictionRecreate_RetryInterval_FloorAndDerivation is a
// determinism-of-cadence pin: the retry interval is derived from the
// policy's InitialBackoff (half of it) but never below the
// minRetryTickInterval floor. Without the floor a sub-millisecond
// InitialBackoff would spin the ticker.
func TestEvictionRecreate_RetryInterval_FloorAndDerivation(t *testing.T) {
	mux, _ := newStreamMuxForEvictTest(t)

	// Default policy: InitialBackoff=5s → interval=2.5s (well above
	// the 100ms floor).
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{}
	require.Equal(t, 2500*time.Millisecond, mux.evictionRecreateRetryInterval(),
		"default-policy interval must be InitialBackoff/2 = 2.5s")

	// Tight test policy: InitialBackoff=10ms → would derive 5ms but
	// the 100ms floor wins.
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff: 10 * time.Millisecond,
	}
	require.Equal(t, minRetryTickInterval, mux.evictionRecreateRetryInterval(),
		"sub-floor InitialBackoff must clamp to minRetryTickInterval")

	// Mid-range: InitialBackoff=400ms → 200ms, above the floor.
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff: 400 * time.Millisecond,
	}
	require.Equal(t, 200*time.Millisecond, mux.evictionRecreateRetryInterval(),
		"InitialBackoff/2 above the floor must be honored")
}

// TestEvictionRecreate_RetryTick_RespectsPermanentlyFailed pins the
// AlreadyPermanent gate on the retry path: after MaxAttempts has
// latched, the retry tick must not fire even if the backoff has
// elapsed.
func TestEvictionRecreate_RetryTick_RespectsPermanentlyFailed(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        50 * time.Millisecond,
		MaxAttempts:       2,
		MaxAge:            time.Hour,
	}

	const deadID OutputID = 42
	// Non-empty VideoCodec — see TestEvictionRecreate_PeriodicRetryFiresWithoutNewEviction.
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	})

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("simulated persistent fault")
	}

	rearm := func() {
		mux.Outputs.Store(deadID, dead)
		mux.OutputsMap.Store(dead.StorageKey(), dead)
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	}

	// Drive both attempts to hit MaxAttempts=2 → permanentlyFailed.
	rearm()
	mux.evictDeadOutput(ctx, dead) // attempt 1
	require.Equal(t, 1, calls)

	now = now.Add(25 * time.Millisecond)
	rearm()
	mux.evictDeadOutput(ctx, dead) // attempt 2 = final
	require.Equal(t, 2, calls)

	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.StorageKey()]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, state.permanentlyFailed, "MaxAttempts=2 must latch permanentlyFailed")

	// Re-arm the input so the still-demoted gate would otherwise
	// pass — the AlreadyPermanent gate must still suppress.
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(math.MinInt32))

	// Tick well past any backoff — the AlreadyPermanent gate must
	// suppress regardless.
	now = now.Add(time.Second)
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 2, calls, "retry tick must NOT fire while permanentlyFailed is latched")
}

// TestEvictionRecreate_RetryTick_NotAllowedDifferentOutputs pins the
// MuxMode guard: in modes that don't support per-input switching, the
// retry tick is a no-op. This avoids polluting logs with "orphaned
// input" warnings for a recovery path that cannot run anyway.
func TestEvictionRecreate_RetryTick_NotAllowedDifferentOutputs(t *testing.T) {
	ctx := context.Background()
	// MuxModeForbid: IsAllowedDifferentOutputs() returns false.
	mux, err := NewWithCustomData[struct{}](ctx, types.MuxModeForbid, dummyOutputFactory{})
	require.NoError(t, err)

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return nil
	}

	// Even with a state entry primed, the tick must short-circuit at
	// the IsAllowedDifferentOutputs() guard.
	mux.evictionRecreateLocker.Lock()
	mux.lastEvictionRecreateState[SenderKey{VideoCodec: codectypes.Name("av1")}] = evictionRecreateState{
		lastFailureTime:     time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC).Add(-time.Hour),
		consecutiveFailures: 1,
	}
	mux.evictionRecreateLocker.Unlock()

	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, 0, calls, "retry tick must be a no-op when MuxMode does not allow per-input switching")
}
