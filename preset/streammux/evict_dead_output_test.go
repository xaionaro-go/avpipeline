// evict_dead_output_test.go verifies StreamMux.evictDeadOutput detaches
// a dead output from every routing structure that AutoBitRateHandler /
// withActiveVideoOutput could otherwise resolve back to.
package streammux

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

// newStreamMuxForEvictTest builds a StreamMux with both audio-only and
// video-only inputs (SplitAV mode) so the test can assert the demotion
// path runs across multiple inputs without spinning up the Serve
// goroutines.
func newStreamMuxForEvictTest(t *testing.T) (*StreamMux[struct{}], context.Context) {
	t.Helper()
	ctx := context.Background()
	mux, err := NewWithCustomData[struct{}](
		ctx,
		types.MuxModeDifferentOutputsSameTracksSplitAV,
		dummyOutputFactory{},
	)
	require.NoError(t, err)
	require.NotNil(t, mux.InputVideoOnly)
	require.NotNil(t, mux.InputAudioOnly)
	return mux, ctx
}

func newDeadOutputForTest(
	t *testing.T,
	ctx context.Context,
	mux *StreamMux[struct{}],
	outputID OutputID,
) *Output[struct{}] {
	t.Helper()
	return newOutputForInputForTest(t, ctx, mux.InputVideoOnly, outputID, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})
}

// newOutputForInputForTest constructs a synthetic Output bound to a
// specific Input and SenderKey so the test can build sibling outputs
// (different keys, same input) for the recommit-to-sibling assertions.
func newOutputForInputForTest(
	t *testing.T,
	ctx context.Context,
	input *Input[struct{}],
	outputID OutputID,
	key SenderKey,
) *Output[struct{}] {
	t.Helper()
	out, err := newOutput[struct{}](
		ctx,
		outputID,
		input.Node,
		dummyOutputFactory{},
		key,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		initOutputConfig{},
	)
	require.NoError(t, err)
	return out
}

// TestEvictDeadOutput_RemovesFromMaps_AndDemotesSwitch is the dual-sided
// proof for the eviction fix:
//
//   - GOOD-side: after eviction, s.Outputs / s.OutputsMap no longer
//     resolve to the dead output, and the OutputSwitch / OutputSyncer
//     CurrentValue that had committed to it is back at math.MinInt32 so
//     setPreferredOutputForInput can advance instead of seeing
//     ErrSwitchAlreadyInProgress / ErrOutputAlreadyPreferred.
//   - BAD-side: switches that pointed at a *different* output are NOT
//     demoted (the eviction is targeted, not a global reset), and the
//     map removal is a CompareAndDelete so a freshly-stored newer output
//     under the same SenderKey is preserved.
func TestEvictDeadOutput_RemovesFromMaps_AndDemotesSwitch(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const liveID OutputID = 7

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// The audio input is committed to a different (live) output ID — its
	// switches must survive the eviction untouched.
	mux.InputAudioOnly.OutputSwitch.CurrentValue.Store(int32(liveID))
	mux.InputAudioOnly.OutputSyncer.CurrentValue.Store(int32(liveID))

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: dead output is gone from both maps.
	_, okOutputs := mux.Outputs.Load(deadID)
	require.False(t, okOutputs, "dead output must be removed from Outputs (OutputID-keyed)")
	_, okOutputsMap := mux.OutputsMap.Load(dead.GetKey())
	require.False(t, okOutputsMap, "dead output must be removed from OutputsMap (SenderKey-keyed)")

	// GOOD-side: the video switches that pointed at the dead output are
	// demoted to math.MinInt32, so withActiveVideoOutput will no longer
	// resolve to it and AutoBitRateHandler can stop livelocking.
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"OutputSwitch.CurrentValue on the video input must be demoted to MinInt32",
	)
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"OutputSyncer.CurrentValue on the video input must be demoted to MinInt32",
	)

	// BAD-side: the audio switches were committed to a different live
	// output. Eviction must not touch them.
	require.Equal(
		t,
		int32(liveID),
		mux.InputAudioOnly.OutputSwitch.CurrentValue.Load(),
		"OutputSwitch.CurrentValue on the audio input must not be demoted (it pointed at a different live output)",
	)
	require.Equal(
		t,
		int32(liveID),
		mux.InputAudioOnly.OutputSyncer.CurrentValue.Load(),
		"OutputSyncer.CurrentValue on the audio input must not be demoted (it pointed at a different live output)",
	)
}

// TestEvictDeadOutput_PreservesNewerEntryUnderSameKey verifies the
// CompareAndDelete semantics: if a fresh GetOrCreateOutput stored a new
// output under the same SenderKey before the rawErrCh handler got
// around to evicting the old one, the newer entry must survive.
func TestEvictDeadOutput_PreservesNewerEntryUnderSameKey(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const oldID OutputID = 1
	const newID OutputID = 2

	old := newDeadOutputForTest(t, ctx, mux, oldID)
	fresh := newDeadOutputForTest(t, ctx, mux, newID)
	require.Equal(t, old.GetKey(), fresh.GetKey(), "test setup expects same SenderKey")

	// Race scenario: rawErrCh handler is about to evict `old`, but
	// GetOrCreateOutput already replaced the SenderKey entry with `fresh`.
	mux.Outputs.Store(oldID, old)
	mux.OutputsMap.Store(fresh.GetKey(), fresh) // fresh wins the SenderKey slot

	mux.evictDeadOutput(ctx, old)

	// CompareAndDelete keyed on `old` must not remove `fresh`.
	got, ok := mux.OutputsMap.Load(fresh.GetKey())
	require.True(t, ok, "fresh output under the same SenderKey must survive eviction of old output")
	require.Same(t, fresh, got)

	// `old` still must be gone from the OutputID-keyed map.
	_, okOld := mux.Outputs.Load(oldID)
	require.False(t, okOld, "old output (OutputID-keyed) must be removed")
}

// TestHandleOutputNodeError_ActiveOutput_EvictsAndForwards is the binding
// proof. When an output that is currently active on at least one input
// emits an error, handleOutputNodeError must:
//
//   - GOOD-side: detach the output from s.Outputs / s.OutputsMap and
//     demote OutputSwitch / OutputSyncer CurrentValue to math.MinInt32
//     (otherwise getActiveVideoOutputLocked still resolves to the
//     orphaned chain whose serving goroutines have already exited and
//     AutoBitRateHandler.withActiveVideoOutput livelocks at 0 bps).
//   - GOOD-side: still return the underlying error so the caller forwards
//     it on errCh — error propagation contract is preserved.
//   - BAD-side: must NOT call output.CloseNoDrain (the active branch
//     leaves teardown to whoever observes the upward error). Verified
//     indirectly via the test passing without panic from a closed
//     output state — see TestHandleOutputNodeError_InactiveOutput for
//     the inactive-branch CloseNoDrain assertion.
//
// Falsification protocol: temporarily comment out the
// `s.evictDeadOutput(ctx, output)` call in handleOutputNodeError's
// `if isActiveOnAnyInput` branch and rerun this test — it MUST fail on
// the OutputSwitch.CurrentValue.Load() assertion.
func TestHandleOutputNodeError_ActiveOutput_EvictsAndForwards(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	// Commit the video input's switch to the dead output so the handler
	// classifies it as active-on-some-input.
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	upstreamErr := errors.New("simulated upstream pipeline error")
	nodeErr := node.Error{
		Node: nil, // only used for log formatting in this branch
		Err:  upstreamErr,
	}

	forwardErr := mux.handleOutputNodeError(ctx, dead, nodeErr)

	// GOOD-side: error must be forwarded upward (not nil).
	require.ErrorIs(t, forwardErr, upstreamErr,
		"handleOutputNodeError must return the underlying error so Serve forwards it on errCh")

	// GOOD-side: dead output is gone from both lookup tables.
	_, okOutputs := mux.Outputs.Load(deadID)
	require.False(t, okOutputs, "active-branch eviction must remove the dead output from Outputs")
	_, okOutputsMap := mux.OutputsMap.Load(dead.GetKey())
	require.False(t, okOutputsMap, "active-branch eviction must remove the dead output from OutputsMap")

	// GOOD-side: switches that pointed at the dead output are demoted —
	// without this, AutoBitRateHandler.withActiveVideoOutput livelocks.
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"active-branch eviction must demote OutputSwitch.CurrentValue to MinInt32",
	)
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"active-branch eviction must demote OutputSyncer.CurrentValue to MinInt32",
	)
}

// TestHandleOutputNodeError_InactiveOutput_EvictsAndSwallows verifies the
// inactive-branch contract is preserved by the extraction: the dead
// output is evicted, CloseNoDrain is invoked (idempotent on a freshly-
// constructed Output), and the function returns nil so the caller does
// NOT forward the error upward.
func TestHandleOutputNodeError_InactiveOutput_EvictsAndSwallows(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 99
	const liveID OutputID = 7

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	// No input commits to deadID — both switch to a different live ID.
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(liveID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(liveID))
	mux.InputAudioOnly.OutputSwitch.CurrentValue.Store(int32(liveID))
	mux.InputAudioOnly.OutputSyncer.CurrentValue.Store(int32(liveID))

	nodeErr := node.Error{
		Node: nil,
		Err:  errors.New("upstream EOF-like error"),
	}

	forwardErr := mux.handleOutputNodeError(ctx, dead, nodeErr)

	// Inactive branch swallows the error.
	require.NoError(t, forwardErr,
		"inactive-output errors must be swallowed (return nil so caller does not forward)")

	// Eviction still ran.
	_, okOutputs := mux.Outputs.Load(deadID)
	require.False(t, okOutputs, "inactive-branch must also evict from Outputs")
	_, okOutputsMap := mux.OutputsMap.Load(dead.GetKey())
	require.False(t, okOutputsMap, "inactive-branch must also evict from OutputsMap")

	// Live switches must survive.
	require.Equal(t, int32(liveID), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load())
	require.Equal(t, int32(liveID), mux.InputAudioOnly.OutputSwitch.CurrentValue.Load())
}

// TestEvictDeadOutput_RecommitsToSurvivingSibling is the dual-sided
// proof for the wedge fix (primary side). When an output errors but a
// surviving sibling output is attached to the same Input,
// evictDeadOutput must recommit the input's OutputSwitch /
// OutputSyncer to that sibling rather than leaving them at MinInt32 —
// otherwise the SwitchOutput.GetState defense-in-depth path drops every
// packet and the sibling cannot start receiving.
//
// Falsification protocol: comment out the
// `s.recommitDemotedInputToSibling(ctx, input, output)` call inside
// evictDeadOutput's ForEachInput body and rerun this test — it MUST
// fail on the OutputSwitch.CurrentValue assertion (CurrentValue would
// stay at math.MinInt32).
func TestEvictDeadOutput_RecommitsToSurvivingSibling(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42
	const siblingID OutputID = 43

	dead := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, deadID, SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	})
	// Different SenderKey so OutputsMap can hold both entries; same
	// InputFrom so the sibling is recognized as attached to the same
	// Input.
	sibling := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, siblingID, SenderKey{
		VideoCodec:      "av1",
		VideoResolution: codectypes.Resolution{Width: 1280, Height: 720},
	})
	require.NotEqual(t, dead.GetKey(), sibling.GetKey(), "test setup must use distinct SenderKeys")

	mux.Outputs.Store(deadID, dead)
	mux.Outputs.Store(siblingID, sibling)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.OutputsMap.Store(sibling.GetKey(), sibling)

	// The video input is committed to the dead output. The recommit path
	// is gated on demotedSwitch||demotedSyncer firing for this input.
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: recommitted to the surviving sibling instead of being
	// stuck at math.MinInt32 (the wedge-prone steady state).
	require.Equal(
		t,
		int32(siblingID),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"OutputSwitch.CurrentValue must be recommitted to the sibling output",
	)
	// OnAfterSwitch (via setPreferredOutputForInput → SetValue → setValueNow)
	// also commits the OutputSyncer to the sibling.
	require.Equal(
		t,
		int32(siblingID),
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"OutputSyncer.CurrentValue must be recommitted to the sibling output",
	)

	// BAD-side: the sibling itself must remain in the lookup tables —
	// the recommit must not accidentally clobber a still-live entry.
	got, ok := mux.Outputs.Load(siblingID)
	require.True(t, ok, "sibling must remain in Outputs after recommit")
	require.Same(t, sibling, got)
	gotByKey, okByKey := mux.OutputsMap.Load(sibling.GetKey())
	require.True(t, okByKey, "sibling must remain in OutputsMap after recommit")
	require.Same(t, sibling, gotByKey)
}

// TestEvictDeadOutput_NoSibling_RecreateFailureLeavesDemoted verifies
// the failure-path contract of the no-sibling recreate. When
// the recreate function returns an error (e.g. the test's empty
// CurrentOutputProps.TranscoderConfig has no VideoCodec, so
// createAndConfigureOutputs is a no-op in SplitAV mode and the
// follow-on setPreferredOutputForInput cannot find the output it tried
// to create), evictDeadOutput leaves the OutputSwitch / OutputSyncer at
// math.MinInt32. The SwitchOutput.GetState defense-in-depth check still
// keeps the pipeline draining via StateDrop until the next eviction
// rearms the recreate.
//
// This guards the failure-mode side of the recreate path; see
// TestEvictDeadOutput_NoSibling_RecreatesOutput for the success-mode
// proof.
func TestEvictDeadOutput_NoSibling_RecreateFailureLeavesDemoted(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: recreate failed (empty cfg) → demoted state preserved
	// (the GetState defense-in-depth path will Drop on this state).
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"with no sibling and a failed recreate, OutputSwitch.CurrentValue must remain demoted to MinInt32",
	)
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"with no sibling and a failed recreate, OutputSyncer.CurrentValue must remain demoted to MinInt32",
	)
}

// TestEvictDeadOutput_NoSibling_RecreatesOutput is the positive proof
// for the recreate path: when an output errors with no surviving
// sibling, evictDeadOutput must materialise a fresh Output under the
// same SenderKey and switch the orphaned input onto it.
//
// Falsification protocol: revert the handleNoSiblingEviction call in
// recommitDemotedInputToSibling (or the recreateEvictedOutputFunc call
// inside it) — this test MUST fail on the recreate-call assertion.
func TestEvictDeadOutput_NoSibling_RecreatesOutput(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// Override the recreate hook so the test asserts on call timing
	// without standing up a real Transcoder/Encoder factory chain.
	type recreateCall struct {
		input *Input[struct{}]
		key   SenderKey
	}
	var calls []recreateCall
	mux.recreateEvictedOutputFunc = func(_ context.Context, input *Input[struct{}], key SenderKey) error {
		calls = append(calls, recreateCall{input: input, key: key})
		return nil
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: recreate hook fired exactly once for the orphaned
	// video input under the dead output's SenderKey.
	require.Len(t, calls, 1, "no-sibling eviction must invoke recreate hook exactly once")
	require.Same(t, mux.InputVideoOnly, calls[0].input, "recreate must be called for the orphaned video input")
	require.Equal(t, dead.GetKey(), calls[0].key, "recreate must be called with the dead output's SenderKey")

	// BAD-side: a backoff timestamp must be recorded so a follow-up
	// eviction within the window does NOT recreate again. Reading
	// under the lock to mirror production.
	mux.evictionRecreateLocker.Lock()
	state, hasLast := mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, hasLast, "recreate-attempt state must be recorded for the dead SenderKey to gate the backoff")
	require.Equal(t, 1, state.consecutiveFailures, "first attempt must bump the counter to 1")
	require.False(t, state.permanentlyFailed, "single attempt must not yet retire the recreate path")
}

// TestEvictDeadOutput_NoSibling_RecentFailure_SkipsRecreate is the
// dual-sided proof for the recreate backoff: a SenderKey that already
// saw a recreate-attempt within the exponential-backoff wait must NOT
// trigger another attempt on the next eviction, otherwise an Output
// that fails to recover after recreation would burn CPU in a tight
// recreate-and-die loop.
//
// Falsification protocol: remove the SkipBackoff branch in
// evaluateEvictionRecreateLocked — this test MUST fail on the
// recreate-call length assertion (the hook would fire despite the
// recent attempt).
func TestEvictDeadOutput_NoSibling_RecentFailure_SkipsRecreate(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// Pin the clock so the test is deterministic.
	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	// Prime a recent (within-backoff) attempt under the dead key so
	// the gate trips. consecutiveFailures=1 → backoff for next attempt
	// = InitialBackoff*Multiplier^1 = 5s*2 = 10s. 1s elapsed << 10s.
	mux.evictionRecreateLocker.Lock()
	mux.lastEvictionRecreateState[dead.GetKey()] = evictionRecreateState{
		lastFailureTime:     now.Add(-1 * time.Second),
		consecutiveFailures: 1,
	}
	mux.evictionRecreateLocker.Unlock()

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return nil
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: backoff suppressed the recreate hook.
	require.Equal(t, 0, calls, "recent recreate attempt must suppress a follow-up recreate within the backoff window")

	// BAD-side: the demoted-only state is preserved so the GetState
	// defense-in-depth path drains via StateDrop until the backoff
	// window elapses and a new eviction tick rearms.
	require.Equal(
		t,
		int32(math.MinInt32),
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"with backoff active, OutputSwitch.CurrentValue must remain demoted",
	)
}

// TestEvictDeadOutput_NoSibling_StaleFailure_RecreatesOutput verifies
// that once the backoff window elapses, the next eviction rearms the
// recreate path so a longer-lasting stall (the underlying root cause
// goes away by the time the next eviction happens) still recovers.
//
// Falsification protocol: change the SkipBackoff comparison from `<`
// to `<=` so freshly-stale timestamps are also skipped — this test
// MUST fail because no recreate would fire.
func TestEvictDeadOutput_NoSibling_StaleFailure_RecreatesOutput(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	// Prime a stale (older than the consecutiveFailures=1 backoff =
	// 10s) attempt at -30s — well past the 10s gate but well below the
	// 5m MaxAge sliding-window reset, so the counter does NOT reset.
	// This is the test witness for the (elapsed >= backoff) AND
	// (elapsed <= MaxAge) branch.
	mux.evictionRecreateLocker.Lock()
	mux.lastEvictionRecreateState[dead.GetKey()] = evictionRecreateState{
		lastFailureTime:     now.Add(-30 * time.Second),
		consecutiveFailures: 1,
	}
	mux.evictionRecreateLocker.Unlock()

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return nil
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: stale timestamp does not gate; recreate fires.
	require.Equal(t, 1, calls, "stale recreate-attempt timestamp must allow a follow-up recreate after the backoff window elapses")

	// BAD-side: timestamp is refreshed to `now`, gating the next
	// eviction within the new (longer) backoff window. Counter bumps
	// to 2 because we did NOT cross the MaxAge sliding-window reset.
	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.Equal(t, now, state.lastFailureTime, "recreate-attempt timestamp must be refreshed to now after a fresh recreate")
	require.Equal(t, 2, state.consecutiveFailures, "non-reset stale-but-not-quiescent attempt must bump the counter to 2 (was 1)")
}

// TestEvictionRecreate_ConfigOverrideBackoff is the dual-sided proof:
// the EvictionRecreatePolicy.InitialBackoff field must override the 5s
// default. Without the field plumbed through applyDefaults+backoffFor,
// a deployment that wants tight retry (e.g. a low-latency mediacodec
// recovery test bench) cannot exercise the gate at sub-second
// granularity.
//
// Falsification protocol: hard-code backoffFor to ignore InitialBackoff
// and always return defaultEvictionRecreateInitialBackoff — this test
// MUST fail because the second recreate would be gated by the 5s
// default instead of clearing the 100ms override.
func TestEvictionRecreate_ConfigOverrideBackoff(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff: 100 * time.Millisecond,
	}

	const deadID OutputID = 42

	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	base := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	currentNow := base
	mux.nowFunc = func() time.Time { return currentNow }

	// Prime an attempt 50ms in the past, consecutiveFailures=0. The
	// backoff at consecutiveFailures=0 is InitialBackoff (100ms under
	// the override, 5s under the legacy default). 50ms < 100ms <
	// 5s — only the override gates this tick.
	mux.evictionRecreateLocker.Lock()
	mux.lastEvictionRecreateState[dead.GetKey()] = evictionRecreateState{
		lastFailureTime:     base.Add(-50 * time.Millisecond),
		consecutiveFailures: 0,
	}
	mux.evictionRecreateLocker.Unlock()

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return nil
	}

	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: 50ms < 100ms override → backoff gate trips → no
	// recreate. (Under the legacy hard-coded 5s default this would
	// also gate, but the assertion below proves the override actually
	// shortens the window vs. the default.)
	require.Equal(t, 0, calls, "100ms override gate must suppress a recreate attempt 50ms after the previous one")

	// Reinstall the dead output so the eviction-recovery path re-arms
	// for the next tick — the previous evictDeadOutput already removed
	// it from the maps + demoted the switches to MinInt32.
	mux.Outputs.Store(deadID, dead)
	mux.OutputsMap.Store(dead.GetKey(), dead)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))

	// Advance past the 100ms override but still well within the 5s
	// default. The recreate MUST fire — proving the override is read,
	// not the default.
	currentNow = base.Add(150 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)

	// BAD-side: with the override read, the gate has elapsed at
	// 150ms > 100ms → recreate fires. Without the override (legacy
	// 5s), 150ms < 5s would still gate and `calls` would stay 0.
	require.Equal(t, 1, calls, "150ms after a primed attempt must clear the 100ms override gate (would still be gated under the 5s default)")
}

// TestEvictionRecreate_ExponentialBackoffEscalates is the dual-sided
// proof for the exponential-backoff escalation: each consecutive
// failure increases the wait before the next attempt by
// BackoffMultiplier, capped at MaxBackoff. Without the escalation,
// the gate stays at InitialBackoff forever — defeating the
// "ramp pacing under persistent fault" intent.
//
// Falsification protocol: change backoffFor to return
// p.InitialBackoff regardless of consecutiveFailures — this test
// MUST fail at attempt 3 because the 50ms gap that should clear the
// 40ms backoff (n=2) would also clear the 10ms initial backoff,
// firing one extra time before the cap.
func TestEvictionRecreate_ExponentialBackoffEscalates(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	// Tight + escalating: 10ms, 20ms, 40ms, 80ms→cap60ms, 60ms, ...
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        60 * time.Millisecond,
		MaxAttempts:       100, // out of the way for this test
		MaxAge:            time.Hour,
	}

	const deadID OutputID = 42
	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("persistent fault")
	}

	rearm := func() {
		mux.Outputs.Store(deadID, dead)
		mux.OutputsMap.Store(dead.GetKey(), dead)
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	}

	// Attempt 1 (fresh state): no backoff gate, fires.
	rearm()
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 1, calls, "attempt 1 fires on fresh state")

	// At consecutiveFailures=1, backoff=20ms. A 5ms step should NOT
	// clear it.
	rearm()
	now = now.Add(5 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 1, calls, "5ms < 20ms backoff (n=1) must gate")

	// 20ms total elapsed clears the 20ms gate → attempt 2 fires.
	rearm()
	now = now.Add(15 * time.Millisecond) // total 20ms
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "20ms clears the 20ms backoff (n=1)")

	// At consecutiveFailures=2, backoff=40ms. A 25ms step does NOT
	// clear it; a 45ms step does.
	rearm()
	now = now.Add(25 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "25ms < 40ms backoff (n=2) must gate")
	rearm()
	now = now.Add(20 * time.Millisecond) // total 45ms since attempt 2
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 3, calls, "45ms clears the 40ms backoff (n=2)")

	// At consecutiveFailures=3, backoff=80ms→capped to 60ms. A 50ms
	// step does NOT clear; a 65ms step does. The cap is what makes
	// this assertion fail under a missing MaxBackoff cap (uncapped
	// 80ms would still gate at 65ms).
	rearm()
	now = now.Add(50 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 3, calls, "50ms < 60ms backoff (n=3, capped) must gate")
	rearm()
	now = now.Add(15 * time.Millisecond) // total 65ms since attempt 3
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 4, calls, "65ms clears the 60ms backoff (n=3, capped from 80ms)")

	// At consecutiveFailures=4, backoff=160ms→capped to 60ms (same as
	// n=3). Verify the cap stays put: 65ms clears even at higher n.
	rearm()
	now = now.Add(65 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 5, calls, "65ms clears the 60ms-capped backoff (n=4)")
}

// TestEvictionRecreate_MaxAttemptsTerminalFailure is the dual-sided
// proof for the MaxAttempts ceiling: after MaxAttempts consecutive
// failures, the SenderKey is marked permanentlyFailed and subsequent
// recreates are suppressed.
//
// Falsification protocol: remove the `state.consecutiveFailures >=
// policy.MaxAttempts` branch — this test MUST fail because attempt 3
// (post-cap) would still reach the hook instead of being suppressed
// by the AlreadyPermanent path.
func TestEvictionRecreate_MaxAttemptsTerminalFailure(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        100 * time.Millisecond,
		MaxAttempts:       2, // cap at 2 attempts
		MaxAge:            time.Hour,
	}

	const deadID OutputID = 42
	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("simulated persistent encoder fault")
	}

	rearm := func() {
		mux.Outputs.Store(deadID, dead)
		mux.OutputsMap.Store(dead.GetKey(), dead)
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	}

	// Attempt 1: fresh state, fires.
	rearm()
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 1, calls, "attempt 1 fires (fresh state, below MaxAttempts=2)")

	// Attempt 2: clears the 20ms backoff (n=1). This is the
	// MaxAttempts-th attempt — fires AND latches permanentlyFailed.
	rearm()
	now = now.Add(25 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "attempt 2 fires (final attempt at MaxAttempts cap)")

	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, state.permanentlyFailed, "permanentlyFailed must latch after the MaxAttempts-th failure")
	require.Equal(t, 2, state.consecutiveFailures, "consecutiveFailures must equal MaxAttempts after the final attempt")

	// Attempt 3: post-cap. AlreadyPermanent branch must suppress the
	// hook regardless of how much time elapses (as long as < MaxAge).
	rearm()
	now = now.Add(500 * time.Millisecond) // far past any backoff but << MaxAge
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "post-cap tick must not invoke the recreate hook")

	mux.evictionRecreateLocker.Lock()
	state = mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, state.permanentlyFailed, "permanentlyFailed must remain latched after AlreadyPermanent ticks")
	require.Equal(t, 2, state.consecutiveFailures, "AlreadyPermanent ticks must not bump consecutiveFailures")

	// One more post-cap tick to confirm idempotence.
	rearm()
	now = now.Add(500 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "second post-cap tick must also be suppressed")
}

// TestEvictionRecreate_SlidingWindowResetsAfterMaxAge is the dual-
// sided proof for the MaxAge sliding-window reset: a SenderKey whose
// lastFailureTime is older than MaxAge gets a fresh state on the next
// eviction (consecutiveFailures and permanentlyFailed both reset),
// allowing a once-retired SenderKey to recover after the underlying
// fault has had time to clear.
//
// Falsification protocol: remove the (now.Sub(state.lastFailureTime)
// > policy.MaxAge) reset branch — this test MUST fail because the
// post-MaxAge tick would hit AlreadyPermanent instead of a fresh fire.
func TestEvictionRecreate_SlidingWindowResetsAfterMaxAge(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.EvictionRecreatePolicy = EvictionRecreatePolicy{
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        100 * time.Millisecond,
		MaxAttempts:       2,
		MaxAge:            500 * time.Millisecond, // tight reset window for the test
	}

	const deadID OutputID = 42
	dead := newDeadOutputForTest(t, ctx, mux, deadID)

	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mux.nowFunc = func() time.Time { return now }

	calls := 0
	mux.recreateEvictedOutputFunc = func(_ context.Context, _ *Input[struct{}], _ SenderKey) error {
		calls++
		return errors.New("persistent fault")
	}

	rearm := func() {
		mux.Outputs.Store(deadID, dead)
		mux.OutputsMap.Store(dead.GetKey(), dead)
		mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(deadID))
		mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(deadID))
	}

	// Drive MaxAttempts=2 failures so the SenderKey is retired.
	rearm()
	mux.evictDeadOutput(ctx, dead)
	rearm()
	now = now.Add(25 * time.Millisecond)
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "two attempts must fire before the cap")

	mux.evictionRecreateLocker.Lock()
	state := mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.True(t, state.permanentlyFailed, "permanentlyFailed must latch after MaxAttempts hits")

	// Tick within MaxAge: stays AlreadyPermanent, no recreate.
	rearm()
	now = now.Add(100 * time.Millisecond) // total 125ms < 500ms MaxAge
	mux.evictDeadOutput(ctx, dead)
	require.Equal(t, 2, calls, "tick within MaxAge must stay retired")

	// Advance past MaxAge from the LAST failure time so the sliding
	// window resets the state.
	rearm()
	now = now.Add(600 * time.Millisecond) // > 500ms MaxAge from last failure
	mux.evictDeadOutput(ctx, dead)

	// GOOD-side: post-MaxAge tick rearmed via the sliding-window
	// reset → recreate fires again.
	require.Equal(t, 3, calls, "post-MaxAge tick must rearm and fire a fresh recreate")

	// BAD-side: state was reset before the bump, so consecutiveFailures
	// is now 1 (not 3) and permanentlyFailed has been cleared.
	mux.evictionRecreateLocker.Lock()
	state = mux.lastEvictionRecreateState[dead.GetKey()]
	mux.evictionRecreateLocker.Unlock()
	require.False(t, state.permanentlyFailed, "permanentlyFailed must be cleared by the sliding-window reset")
	require.Equal(t, 1, state.consecutiveFailures, "consecutiveFailures must reset to 1 (= 0 reset + 1 bump)")
}
