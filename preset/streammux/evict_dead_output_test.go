// evict_dead_output_test.go verifies StreamMux.evictDeadOutput detaches
// a dead output from every routing structure that AutoBitRateHandler /
// withActiveVideoOutput could otherwise resolve back to.
package streammux

import (
	"context"
	"errors"
	"math"
	"testing"

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

	// BAD-side: lastEvictedKey must be recorded for the orphaned input
	// so the 1 Hz retry tick can pick up where this synchronous attempt
	// leaves off if the recreate fails.
	gotKey, hasKey := mux.lastEvictedKeyFor(mux.InputVideoOnly)
	require.True(t, hasKey, "lastEvictedKey must be recorded for the orphaned input so the retry tick can recover it")
	require.Equal(t, dead.GetKey(), gotKey, "lastEvictedKey must hold the dead output's SenderKey")
}
