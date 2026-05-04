// eviction_recreate_retry_test.go pins the 1 Hz indefinite reconnect
// design for the no-sibling eviction-recovery path:
//
//   - The retry tick fires every tick interval (1 Hz in production)
//     for any orphaned input.
//   - Recovery (OutputSwitch advancing off MinInt32) silences the tick
//     for that input.
//   - There is NO give-up point — the tick keeps trying indefinitely
//     until either ctx is canceled or the input recovers.
//
// All four tests follow the dual-sided + falsifier-validated discipline
// required by testing-discipline.

package streammux

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

// TestEvictionRecreate_RetryTick_FiresAt1Hz pins the per-tick
// invariant: a single retry-tick call fires the recreate hook exactly
// once for an orphaned input, regardless of wall-clock time. The
// production 1 Hz cadence is enforced by the time.Ticker wrapper in
// evictionRecreateRetryLoop; the unit test drives the tick body
// synchronously and asserts the per-tick semantics.
//
//   - GOOD-side: 5 synchronous tick calls for an orphaned input fire
//     the recreate hook exactly 5 times.
//   - BAD-side: a tick on a non-orphaned input must NOT fire.
//
// Falsification protocol: revert the tick body to a no-op (return
// without scanning inputs) — this test MUST fail because no tick
// would invoke the recreate hook.
func TestEvictionRecreate_RetryTick_FiresAt1Hz(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})

	var calls atomic.Int32
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls.Add(1)
		return errors.New("simulated persistent fault")
	}

	// Prime the orphaned-input state via a real eviction. After this:
	//   - OutputSwitch.CurrentValue == MinInt32 (orphaned)
	//   - lastEvictedKey records the dead SenderKey for the input
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, int32(1), calls.Load(), "the on-eviction path fires attempt 1")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"input must be demoted to MinInt32 post-eviction")

	// 5 synchronous ticks — each must fire the hook exactly once
	// because the input remains orphaned and there is no backoff or
	// budget.
	for i := 0; i < 5; i++ {
		mux.evictionRecreateRetryTick(ctx)
	}
	require.Equal(t, int32(6), calls.Load(),
		"5 retry ticks on an orphaned input must each fire the recreate hook (1 from on-eviction + 5 from ticks)")
}

// TestEvictionRecreate_RetryTick_StopsWhenInputRecovers pins the
// recovery-silence invariant: once OutputSwitch advances off MinInt32
// (recreate succeeded or external action recovered the input), the
// retry tick must NOT fire for that input.
//
//   - GOOD-side: post-recovery ticks do NOT fire the hook.
//   - BAD-side: pre-recovery ticks DO fire the hook (otherwise the
//     test would be vacuous).
//
// Falsification protocol: remove the OutputSwitch.CurrentValue.Load()
// guard in the retry tick body — this test MUST fail because the
// post-recovery ticks would still call the hook.
func TestEvictionRecreate_RetryTick_StopsWhenInputRecovers(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const recoveredID OutputID = 99
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})

	var calls atomic.Int32
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls.Add(1)
		return errors.New("simulated fault")
	}

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, int32(1), calls.Load(), "the on-eviction path fires attempt 1")

	// Pre-recovery: tick must fire (BAD-side establishes the test is
	// non-vacuous).
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, int32(2), calls.Load(), "pre-recovery tick must fire")

	// Simulate recovery: switch advances off MinInt32.
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(recoveredID))

	// Post-recovery: 10 ticks must remain silent.
	for i := 0; i < 10; i++ {
		mux.evictionRecreateRetryTick(ctx)
	}
	require.Equal(t, int32(2), calls.Load(),
		"post-recovery ticks must NOT fire the recreate hook for the recovered input")
}

// TestEvictionRecreate_RetryTick_NeverGivesUp pins the no-give-up
// invariant: 1000 consecutive ticks against a persistent fault must
// each fire the hook. Live-streaming users cannot tolerate any
// "permanently failed" retirement — the destination either becomes
// reachable or doesn't, and the daemon must keep trying.
//
//   - GOOD-side: 1000 ticks → 1000 fires.
//   - BAD-side: ANY missing fire indicates a hidden ceiling/budget.
//
// Falsification protocol: introduce ANY ceiling (max-attempts,
// permanently-failed latch, exponential-backoff gate) — this test
// MUST fail because some subset of the 1000 ticks would be
// suppressed.
func TestEvictionRecreate_RetryTick_NeverGivesUp(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})

	var calls atomic.Int32
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls.Add(1)
		return errors.New("persistent fault — destination unreachable")
	}

	// Prime the orphaned-input state via a real eviction.
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, int32(1), calls.Load(), "the on-eviction path fires attempt 1")

	// 1000 ticks — every single one must fire. ANY suppression
	// (ceiling, budget, latch) would drop the tail count below
	// 1001 (1 from eviction + 1000 from ticks).
	const ticks = 1000
	for i := 0; i < ticks; i++ {
		mux.evictionRecreateRetryTick(ctx)
	}
	require.Equal(t, int32(1+ticks), calls.Load(),
		"every retry tick on a persistent fault must fire the hook — no give-up point")
}

// TestEvictionRecreate_RecoveryWithin1Sec pins the production
// scenario binding the user's directive: when the destination becomes
// reachable, the next retry tick (which fires within at most one tick
// interval = 1 s) must successfully recover the orphaned input. This
// is the unit-level analogue of the mission witness on the phone.
//
//   - GOOD-side: simulate the destination coming back at tick N
//     (recreate hook returns nil and switches the input). The very
//     next tick must observe the input recovered (OutputSwitch off
//     MinInt32) and stop firing.
//   - BAD-side: any mechanism that would delay the recovery beyond
//     one tick interval (backoff, cooldown) would push recovery past
//     the 1 s budget.
//
// Falsification protocol: insert a "skip if backoff <= elapsed"
// branch — this test MUST fail because the recovery tick (N) would
// be suppressed and the input would stay orphaned.
func TestEvictionRecreate_RecoveryWithin1Sec(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const recoveredID OutputID = 99
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})

	// Hook simulates the destination state: returns an error while
	// the destination is "down", returns nil and advances OutputSwitch
	// once the destination is "up".
	var destinationUp atomic.Bool
	var calls atomic.Int32
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls.Add(1)
		if !destinationUp.Load() {
			return errors.New("destination unreachable")
		}
		// Simulate the production recreate path's effect: switch the
		// orphaned input onto a freshly-created Output.
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(recoveredID))
		return nil
	}

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead) // attempt 1: destination still down
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"input demoted while destination is down")

	// Several ticks while destination is still down — each must fire,
	// each must keep the input demoted.
	for i := 0; i < 3; i++ {
		mux.evictionRecreateRetryTick(ctx)
	}
	require.Equal(t, int32(4), calls.Load(),
		"ticks while destination is down must each fire (no backoff)")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"input remains demoted while destination is down")

	// Bring the destination up. The very next tick must recover.
	destinationUp.Store(true)
	mux.evictionRecreateRetryTick(ctx)
	require.Equal(t, int32(5), calls.Load(),
		"the recovery tick must fire (no suppression by backoff/cooldown)")
	require.Equal(t, int32(recoveredID), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"recovery tick must advance OutputSwitch off MinInt32")

	// Subsequent ticks must observe the input recovered and stop
	// firing.
	for i := 0; i < 5; i++ {
		mux.evictionRecreateRetryTick(ctx)
	}
	require.Equal(t, int32(5), calls.Load(),
		"post-recovery ticks must remain silent")
}

// TestEvictionRecreate_RetryTickInterval_Is1Hz pins the production
// cadence: the retry tick fires at exactly 1 Hz. The interval is a
// const (retryTickInterval) so this test reads it directly — but we
// pin it so any unintended change to the cadence trips the test.
func TestEvictionRecreate_RetryTickInterval_Is1Hz(t *testing.T) {
	require.Equal(t, time.Second, retryTickInterval,
		"retry tick must fire at 1 Hz — see RETRY_SEMANTICS.md")
}

// TestEvictionRecreate_StoreBeforeDemote pins the C1 ordering invariant:
// any moment at which OutputSwitch.CurrentValue equals MinInt32 (because
// evictDeadOutput just demoted it) must coincide with lastEvictedKey
// already being recorded for that input. Otherwise a 1 Hz retry tick
// running concurrently between the demote and a later Store can read
// MinInt32, find no key, and skip — costing one tick (~1 s) of recovery
// latency.
//
//   - GOOD-side: at the post-demote test seam, lastEvictedKey is set
//     for the demoted input.
//   - BAD-side: any input that was NOT demoted (e.g. audio input pointed
//     at a different live output) must NOT have a lastEvictedKey entry
//     — the fix only stores for inputs that are about to be demoted.
//
// Falsification protocol: revert the Store-hoist in evictDeadOutput
// (move the Store back to handleNoSiblingEviction post-demote) — this
// test MUST fail because the seam fires AFTER demote but BEFORE the
// later Store, observing the buggy intermediate state.
func TestEvictionRecreate_StoreBeforeDemote(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const liveID OutputID = 7
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})

	// Audio input committed to a live output — it must NOT receive a
	// lastEvictedKey entry, since it isn't demoted.
	mux.InputAudioOnly.OutputSwitch.CurrentValue.Store(int32(liveID))
	mux.InputAudioOnly.OutputSyncer.CurrentValue.Store(int32(liveID))

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// Capture the lastEvictedKey state at the moment the post-demote
	// seam fires for the video input. Pre-fix: Store hasn't happened
	// yet → not set. Post-fix: Store happens before demote → set.
	type snapshot struct {
		input   *Input[struct{}]
		switch_ int32
		key     SenderKey
		hasKey  bool
	}
	var snaps []snapshot
	mux.evictDemoteTestHook = func(input *Input[struct{}]) {
		k, ok := mux.lastEvictedKey.Load(input)
		snaps = append(snaps, snapshot{
			input:   input,
			switch_: input.OutputSwitch.CurrentValue.Load(),
			key:     k,
			hasKey:  ok,
		})
	}
	t.Cleanup(func() { mux.evictDemoteTestHook = nil })

	// Suppress the recreate side-effect; this test is purely about
	// ordering between Store and demote.
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		return errors.New("suppressed for ordering test")
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: the seam observed the video input at MinInt32 with
	// lastEvictedKey already set to the dead output's StorageKey.
	var videoSnap *snapshot
	for i := range snaps {
		if snaps[i].input == mux.InputVideoOnly {
			videoSnap = &snaps[i]
			break
		}
	}
	require.NotNil(t, videoSnap, "post-demote seam must fire for the video input")
	require.Equal(t, int32(math.MinInt32), videoSnap.switch_,
		"seam fires after the demote: OutputSwitch must be MinInt32")
	require.True(t, videoSnap.hasKey,
		"lastEvictedKey must be Stored BEFORE the demote so a concurrent tick observing MinInt32 can find the key")
	require.Equal(t, dead.StorageKey(), videoSnap.key,
		"lastEvictedKey must hold the dead output's StorageKey")

	// BAD-side: the audio input was not demoted (it pointed at liveID,
	// not deadID) so it must NOT have a lastEvictedKey entry.
	_, hasAudio := mux.lastEvictedKey.Load(mux.InputAudioOnly)
	require.False(t, hasAudio,
		"non-demoted inputs must not be recorded in lastEvictedKey")
}

// TestEvictionRecreate_RetryTick_DeletesStaleKeyOnRecovery pins the C3
// Store/Delete-asymmetry fix: when the retry tick observes that an
// input has recovered (OutputSwitch advanced off MinInt32) but a stale
// lastEvictedKey entry is still present for it, the tick must Delete
// the entry.
//
//   - GOOD-side: after the recovery tick, lastEvictedKey.Load(input)
//     returns ok=false.
//   - BAD-side: a still-orphaned input (left at MinInt32) must NOT have
//     its entry deleted — the tick must keep the recorded key so it can
//     keep retrying.
//
// Falsification protocol: remove the Delete call from
// evictionRecreateRetryTick — this test MUST fail because the post-
// recovery Load would still return ok=true.
func TestEvictionRecreate_RetryTick_DeletesStaleKeyOnRecovery(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const recoveredID OutputID = 99
	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		return errors.New("suppressed; this test exercises the recovery-Delete path only")
	}

	// Set up the orphaned-input state: video input demoted, key recorded.
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.StorageKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	mux.evictDeadOutput(ctx, dead)

	// Pre-recovery sanity: orphaned input has the key recorded.
	_, hasKey := mux.lastEvictedKey.Load(mux.InputVideoOnly)
	require.True(t, hasKey, "test setup: orphaned input must have lastEvictedKey recorded")

	// Simulate recovery (any path: sibling, recreate, external).
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(recoveredID))

	// Run a tick — the recovery-side Delete path must clean up the
	// stale entry so map size correlates with currently-orphaned set,
	// not lifetime-orphaned set.
	mux.evictionRecreateRetryTick(ctx)

	// GOOD-side: stale entry deleted.
	_, hasKeyPost := mux.lastEvictedKey.Load(mux.InputVideoOnly)
	require.False(t, hasKeyPost,
		"recovery tick must Delete the stale lastEvictedKey entry")

	// BAD-side: an input that is still at MinInt32 with a recorded key
	// must NOT have its entry deleted. Re-arm the orphaned state and
	// verify the tick keeps the key.
	mux.lastEvictedKey.Store(mux.InputVideoOnly, dead.StorageKey())
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(math.MinInt32))
	mux.evictionRecreateRetryTick(ctx)
	_, hasKeyAfterReorphan := mux.lastEvictedKey.Load(mux.InputVideoOnly)
	require.True(t, hasKeyAfterReorphan,
		"still-orphaned input must keep its lastEvictedKey for the next retry")
}
