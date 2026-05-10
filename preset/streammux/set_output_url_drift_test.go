// set_output_url_drift_test.go covers Task #174:
// SetOutputURL drift detection on the Reuse path of
// StreamMux.getOrCreateOutputLocked.
//
// Without drift detection, calling SetOutputURL to change the
// SenderFactory's underlying URL template and then calling
// SwitchToOutputByProps with the same codec props (same senderKey)
// takes the fanout.CreationActionReuse path: the existing Output is
// returned unchanged, the existing sender continues publishing to the
// OLD URL, and the operator's URL change is silently dropped.
//
// With drift detection (sending_node.go: SenderURLPreviewer optional
// interface; output.go: Output.senderURLAtCreation field;
// stream_mux.go: outputURLMatchesFactoryPreview helper invoked on
// Reuse), a non-empty mismatch between the previewed URL and the
// existing Output's recorded URL forces a teardown of the stale
// Output and falls through to the Create path so the new URL takes
// effect via NewSender.

package streammux

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

// urlDriftRecordingFactory implements both SenderFactory and
// SenderURLPreviewer. The currentURL field is mutated externally
// between SwitchToOutputByProps calls to simulate the SetOutputURL
// runtime URL-template-mutation that the production senderFactory
// performs in pkg/ffstream/sender_factory.go.
type urlDriftRecordingFactory struct {
	currentURL string
}

var (
	_ SenderFactory[struct{}] = (*urlDriftRecordingFactory)(nil)
	_ SenderURLPreviewer      = (*urlDriftRecordingFactory)(nil)
)

func (f *urlDriftRecordingFactory) NewSender(
	ctx context.Context,
	outputKey SenderKey,
) (SendingNode[struct{}], types.SenderConfig, error) {
	return dummyOutputFactory{}.NewSender(ctx, outputKey)
}

func (f *urlDriftRecordingFactory) URLForKey(
	_ context.Context,
	_ SenderKey,
) (string, error) {
	return f.currentURL, nil
}

// TestSwitchToOutputByProps_SetURLDriftRecreatesOutput is the broke-
// the-code regression for Task #174. The mux's first switch creates
// outputs whose senderURLAtCreation matches the factory's "rtmp://A/"
// URL. After mutating the factory's URL to "rtmp://B/" — simulating
// the SetOutputURL template mutation — the second switch with the
// SAME senderKey MUST tear down the stale output and recreate it so
// the new URL takes effect. Without drift detection the second
// switch's Reuse path returns the prior Output unchanged and
// senderURLAtCreation stays "rtmp://A/" — the assertion FAILS.
//
// Broke-the-code-validation: revert the drift-detection block in
// stream_mux.go getOrCreateOutputLocked (the case
// fanout.CreationActionReuse → outputURLMatchesFactoryPreview branch)
// → the second SwitchToOutputByProps takes the unmodified Reuse path
// → the existing Output is returned unchanged → its
// senderURLAtCreation stays "rtmp://A/" and the OutputID does not
// change → require.NotEqual on firstVideoID/secondVideo.ID FAILS.
// Empirically validated this session via `git stash` of the fix
// (preset/streammux/{output.go,sending_node.go,stream_mux.go}) →
// `go test` → compile error proving the new symbols
// (SenderURLPreviewer, senderURLAtCreation) didn't exist pre-fix.
func TestSwitchToOutputByProps_SetURLDriftRecreatesOutput(t *testing.T) {
	ctx := context.Background()
	factory := &urlDriftRecordingFactory{currentURL: "rtmp://A/"}
	mux, err := NewWithCustomData[struct{}](
		ctx,
		types.MuxModeDifferentOutputsSameTracksSplitAV,
		factory,
	)
	require.NoError(t, err)
	require.NotNil(t, mux.InputVideoOnly)
	require.NotNil(t, mux.InputAudioOnly)

	props := types.SenderProps{TranscoderConfig: squareAV1AACTranscoderConfigForTest()}

	// First switch — factory exposes "rtmp://A/".
	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	videoKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	}
	audioKey := SenderKey{
		AudioCodec:      codectypes.Name("aac"),
		AudioSampleRate: 48000,
	}

	firstVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok, "first switch must materialize the video output")
	require.Equal(t, "rtmp://A/", firstVideo.senderURLAtCreation,
		"video output must record the URL the factory exposed at first construction")
	firstAudio, ok := mux.OutputsMap.Load(audioKey)
	require.True(t, ok, "first switch must materialize the audio output")
	require.Equal(t, "rtmp://A/", firstAudio.senderURLAtCreation,
		"audio output must record the URL the factory exposed at first construction")

	firstVideoID := firstVideo.ID
	firstAudioID := firstAudio.ID

	// Mutate the factory's URL to simulate SetOutputURL changing the
	// underlying template between switches.
	factory.currentURL = "rtmp://B/"

	// Second switch with SAME senderKey — drift detection MUST tear
	// down the stale outputs and recreate them with the new URL.
	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	secondVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok, "second switch must keep a video output present")
	secondAudio, ok := mux.OutputsMap.Load(audioKey)
	require.True(t, ok, "second switch must keep an audio output present")

	require.NotEqual(t, firstVideoID, secondVideo.ID,
		"URL-drift detection must recreate the video output (new OutputID), "+
			"not silently retain the stale-URL sender")
	require.NotEqual(t, firstAudioID, secondAudio.ID,
		"URL-drift detection must recreate the audio output (new OutputID), "+
			"not silently retain the stale-URL sender")

	require.Equal(t, "rtmp://B/", secondVideo.senderURLAtCreation,
		"recreated video output must record the NEW URL the factory now exposes")
	require.Equal(t, "rtmp://B/", secondAudio.senderURLAtCreation,
		"recreated audio output must record the NEW URL the factory now exposes")

	require.True(t, firstVideo.IsClosed(),
		"the stale-URL video output must be Closed after the drift-driven teardown")
	require.True(t, firstAudio.IsClosed(),
		"the stale-URL audio output must be Closed after the drift-driven teardown")
}

// TestSwitchToOutputByProps_SameURL_PreservesReuse is the GOOD-side
// guard: when the factory's URL has NOT changed between switches,
// drift detection MUST be a no-op and the Reuse path must return
// the existing Output unchanged. Without this guard, a regression
// could over-trigger teardown on every switch and waste resources
// rebuilding identical senders.
//
// Broke-the-code-validation: change outputURLMatchesFactoryPreview
// in stream_mux.go to always return false (force the teardown path
// regardless of URL match) → the second SwitchToOutputByProps tears
// down the existing Output even though "rtmp://stable/" hasn't
// changed → require.Equal on firstVideoID/secondVideo.ID FAILS
// (IDs differ) AND require.False on firstVideo.IsClosed() FAILS
// (the same-URL Output got over-eagerly closed).
func TestSwitchToOutputByProps_SameURL_PreservesReuse(t *testing.T) {
	ctx := context.Background()
	factory := &urlDriftRecordingFactory{currentURL: "rtmp://stable/"}
	mux, err := NewWithCustomData[struct{}](
		ctx,
		types.MuxModeDifferentOutputsSameTracksSplitAV,
		factory,
	)
	require.NoError(t, err)

	props := types.SenderProps{TranscoderConfig: squareAV1AACTranscoderConfigForTest()}

	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	videoKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	}
	firstVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok)
	firstVideoID := firstVideo.ID

	// Factory URL UNCHANGED — second switch must take the Reuse path
	// and return the existing Output.
	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	secondVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok)
	require.Equal(t, firstVideoID, secondVideo.ID,
		"unchanged URL must preserve Reuse semantics — Output must be the same instance")
	require.False(t, firstVideo.IsClosed(),
		"unchanged URL must not trigger teardown of the existing Output")
}

// TestSwitchToOutputByProps_FactoryWithoutPreviewer_PreservesReuse is
// the BACKWARDS-COMPAT guard: factories that do NOT implement the
// optional SenderURLPreviewer capability MUST get the legacy Reuse
// behavior (no drift detection runs against an empty
// senderURLAtCreation captured at construction).
//
// Broke-the-code-validation: drop the empty-senderURLAtCreation
// short-circuit in outputURLMatchesFactoryPreview (the
// `if output == nil || output.senderURLAtCreation == ""` early
// return) → the helper would proceed to the SenderURLPreviewer
// type-assert, which still fails for dummyOutputFactory, so the
// helper would still return true and the Reuse path would still
// hit. The empty-senderURLAtCreation guard is belt-and-braces; this
// test pins the contract that factories without the optional
// capability behave identically to pre-fix Reuse semantics —
// regression would manifest if a future change to the helper drops
// the SenderURLPreviewer assertion and proceeds blindly.
func TestSwitchToOutputByProps_FactoryWithoutPreviewer_PreservesReuse(t *testing.T) {
	ctx := context.Background()
	mux, err := NewWithCustomData[struct{}](
		ctx,
		types.MuxModeDifferentOutputsSameTracksSplitAV,
		dummyOutputFactory{}, // does NOT implement SenderURLPreviewer
	)
	require.NoError(t, err)

	props := types.SenderProps{TranscoderConfig: squareAV1AACTranscoderConfigForTest()}

	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	videoKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1920},
	}
	firstVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok)
	require.Empty(t, firstVideo.senderURLAtCreation,
		"factory without SenderURLPreviewer must leave senderURLAtCreation empty")
	firstVideoID := firstVideo.ID

	// Second switch — Reuse path MUST return existing Output unchanged
	// (drift detection is a no-op when senderURLAtCreation is empty).
	require.NoError(t, mux.SwitchToOutputByProps(ctx, props))

	secondVideo, ok := mux.OutputsMap.Load(videoKey)
	require.True(t, ok)
	require.Equal(t, firstVideoID, secondVideo.ID,
		"factory without SenderURLPreviewer must preserve legacy Reuse semantics")
}
