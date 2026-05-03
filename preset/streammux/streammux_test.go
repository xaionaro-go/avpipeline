package streammux

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- Error types ---

func TestErrSwitchAlreadyInProgress(t *testing.T) {
	e := ErrSwitchAlreadyInProgress{OutputIDCurrent: 1, OutputIDNext: 2}
	testifyassert.Contains(t, e.Error(), "switch already in progress")
	testifyassert.Contains(t, e.Error(), "1")
	testifyassert.Contains(t, e.Error(), "2")
}

func TestErrOutputAlreadyPreferred(t *testing.T) {
	e := ErrOutputAlreadyPreferred{OutputID: 5}
	testifyassert.Contains(t, e.Error(), "already preferred")
	testifyassert.Contains(t, e.Error(), "5")
}

func TestErrOutputsAlreadyPreferred(t *testing.T) {
	e := ErrOutputsAlreadyPreferred{OutputIDs: []OutputID{1, 2, 3}}
	testifyassert.Contains(t, e.Error(), "already preferred")
}

func TestErrStop(t *testing.T) {
	e := ErrStop{}
	testifyassert.Equal(t, "stopped", e.Error())
}

func TestErrUnsupportedCodec(t *testing.T) {
	e := ErrUnsupportedCodec{CodecID: astiav.CodecIDH264}
	testifyassert.Contains(t, e.Error(), "unsupported codec")
}

func TestErrNoResolutionsSpecified(t *testing.T) {
	e := ErrNoResolutionsSpecified{}
	testifyassert.Contains(t, e.Error(), "at least one resolution")
}

func TestErrUnableToGetConnectionInfo(t *testing.T) {
	inner := errors.New("some error")
	e := ErrUnableToGetConnectionInfo{
		Processor: "test",
		Err:       inner,
	}
	testifyassert.Contains(t, e.Error(), "unable to get connection info")
	testifyassert.Contains(t, e.Error(), "some error")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrOutputDifferentNotAllowed(t *testing.T) {
	e := ErrOutputDifferentNotAllowed{MuxMode: types.MuxModeForbid}
	testifyassert.Contains(t, e.Error(), "not allowed")
}

func TestErrUnableToDisableBypass(t *testing.T) {
	inner := errors.New("bypass err")
	e := ErrUnableToDisableBypass{Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to disable bypass")
	testifyassert.Contains(t, e.Error(), "bypass err")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrUnableToEnableBypass(t *testing.T) {
	inner := errors.New("bypass err")
	e := ErrUnableToEnableBypass{Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to enable bypass")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrUnableToChangeResolution(t *testing.T) {
	inner := errors.New("res err")
	e := ErrUnableToChangeResolution{Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to change resolution")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrUnableToGetCurrentResolution(t *testing.T) {
	inner := errors.New("get res err")
	e := ErrUnableToGetCurrentResolution{Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to get current resolution")
	testifyassert.Contains(t, e.Error(), "get res err")
	testifyassert.Equal(t, inner, e.Unwrap())

	// With nil error
	e2 := ErrUnableToGetCurrentResolution{}
	testifyassert.Contains(t, e2.Error(), "unable to get current resolution")
	testifyassert.Nil(t, e2.Unwrap())
}

func TestErrNoResolutionConfigFound(t *testing.T) {
	e := ErrNoResolutionConfigFound{Resolution: codec.Resolution{Width: 1920, Height: 1080}}
	testifyassert.Contains(t, e.Error(), "unable to find a resolution config")
}

func TestErrUnableToSetBitrate(t *testing.T) {
	inner := errors.New("br err")
	e := ErrUnableToSetBitrate{Bitrate: 5000000, Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to set bitrate")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrUnableToSetNewResolution(t *testing.T) {
	inner := errors.New("set res err")
	e := ErrUnableToSetNewResolution{
		Resolution: codec.Resolution{Width: 1280, Height: 720},
		Err:        inner,
	}
	testifyassert.Contains(t, e.Error(), "unable to set new resolution")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrResolutionAlreadySet(t *testing.T) {
	e := ErrResolutionAlreadySet{Key: types.SenderKey{}}
	testifyassert.Contains(t, e.Error(), "already set")
}

func TestErrUnableToCompareOutputKeys(t *testing.T) {
	e := ErrUnableToCompareOutputKeys{
		Key1: types.SenderKey{},
		Key2: types.SenderKey{},
	}
	testifyassert.Contains(t, e.Error(), "unable to compare output keys")
}

func TestErrUnableToEnableVideoTranscodingBypass(t *testing.T) {
	inner := errors.New("bypass err")
	e := ErrUnableToEnableVideoTranscodingBypass{Err: inner}
	testifyassert.Contains(t, e.Error(), "unable to enable video transcoding bypass")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrUnableToSetResolution(t *testing.T) {
	inner := errors.New("set res err")
	e := ErrUnableToSetResolution{
		Resolution: codec.Resolution{Width: 640, Height: 480},
		Err:        inner,
	}
	testifyassert.Contains(t, e.Error(), "unable to set resolution")
	testifyassert.Equal(t, inner, e.Unwrap())
}

func TestErrNoSetDropOnClose(t *testing.T) {
	e := ErrNoSetDropOnClose{}
	testifyassert.Contains(t, e.Error(), "does not implement SetDropOnCloser")
}

// --- InputType ---

func TestInputType_String(t *testing.T) {
	testifyassert.Equal(t, "undefined", UndefinedInputType.String())
	testifyassert.Equal(t, "all", InputTypeAll.String())
	testifyassert.Equal(t, "audio-only", InputTypeAudioOnly.String())
	testifyassert.Equal(t, "video-only", InputTypeVideoOnly.String())
	testifyassert.Contains(t, InputType(99).String(), "<unknown_99>")
}

func TestInputType_IncludesMediaType(t *testing.T) {
	// All includes everything
	testifyassert.True(t, InputTypeAll.IncludesMediaType(astiav.MediaTypeVideo))
	testifyassert.True(t, InputTypeAll.IncludesMediaType(astiav.MediaTypeAudio))
	testifyassert.True(t, InputTypeAll.IncludesMediaType(astiav.MediaTypeSubtitle))
	testifyassert.True(t, InputTypeAll.IncludesMediaType(astiav.MediaTypeData))

	// AudioOnly includes audio, subtitle, data but not video
	testifyassert.False(t, InputTypeAudioOnly.IncludesMediaType(astiav.MediaTypeVideo))
	testifyassert.True(t, InputTypeAudioOnly.IncludesMediaType(astiav.MediaTypeAudio))
	testifyassert.True(t, InputTypeAudioOnly.IncludesMediaType(astiav.MediaTypeSubtitle))
	testifyassert.True(t, InputTypeAudioOnly.IncludesMediaType(astiav.MediaTypeData))

	// VideoOnly includes only video
	testifyassert.True(t, InputTypeVideoOnly.IncludesMediaType(astiav.MediaTypeVideo))
	testifyassert.False(t, InputTypeVideoOnly.IncludesMediaType(astiav.MediaTypeAudio))
	testifyassert.False(t, InputTypeVideoOnly.IncludesMediaType(astiav.MediaTypeSubtitle))

	// Undefined includes nothing
	testifyassert.False(t, UndefinedInputType.IncludesMediaType(astiav.MediaTypeVideo))
	testifyassert.False(t, UndefinedInputType.IncludesMediaType(astiav.MediaTypeAudio))
}

// --- Assert helpers ---

func TestMust_NoError(t *testing.T) {
	v := must(42, nil)
	testifyassert.Equal(t, 42, v)
}

func TestMust_WithError(t *testing.T) {
	testifyassert.Panics(t, func() {
		must(0, errors.New("test error"))
	})
}

func TestAssertNoError_NoError(t *testing.T) {
	testifyassert.NotPanics(t, func() {
		assertNoError(nil)
	})
}

func TestAssertNoError_WithError(t *testing.T) {
	testifyassert.Panics(t, func() {
		assertNoError(errors.New("test error"))
	})
}

// --- Utility functions ---

func TestPtr(t *testing.T) {
	v := ptr(42)
	require.NotNil(t, v)
	testifyassert.Equal(t, 42, *v)

	s := ptr("hello")
	require.NotNil(t, s)
	testifyassert.Equal(t, "hello", *s)
}

func TestNanosecondsToDuration(t *testing.T) {
	d := nanosecondsToDuration(1_000_000_000)
	testifyassert.Equal(t, time.Second, d)

	d = nanosecondsToDuration(0)
	testifyassert.Equal(t, time.Duration(0), d)

	d = nanosecondsToDuration(500_000)
	testifyassert.Equal(t, 500*time.Microsecond, d)
}

func TestConvertCustomOptions(t *testing.T) {
	opts := types.DictionaryItems{
		{Key: "key1", Value: "val1"},
		{Key: "key2", Value: "val2"},
	}
	result := convertCustomOptions(opts)
	require.Len(t, result, 2)
	testifyassert.Equal(t, "key1", result[0].Key)
	testifyassert.Equal(t, "val1", result[0].Value)
	testifyassert.Equal(t, "key2", result[1].Key)
	testifyassert.Equal(t, "val2", result[1].Value)
}

func TestConvertCustomOptions_Empty(t *testing.T) {
	result := convertCustomOptions(nil)
	testifyassert.Empty(t, result)
}

// --- multiplyBitRates ---

func TestMultiplyBitRates(t *testing.T) {
	input := []AutoBitRateResolutionAndBitRateConfig{
		{
			Resolution:  codec.Resolution{Width: 1920, Height: 1080},
			BitrateHigh: 8_000_000,
			BitrateLow:  3_000_000,
		},
		{
			Resolution:  codec.Resolution{Width: 1280, Height: 720},
			BitrateHigh: 4_000_000,
			BitrateLow:  2_000_000,
		},
	}
	result := multiplyBitRates(input, 0.5)
	require.Len(t, result, 2)
	testifyassert.Equal(t, types.Ubps(4_000_000), result[0].BitrateHigh)
	testifyassert.Equal(t, types.Ubps(1_500_000), result[0].BitrateLow)
	testifyassert.Equal(t, types.Ubps(2_000_000), result[1].BitrateHigh)
	testifyassert.Equal(t, types.Ubps(1_000_000), result[1].BitrateLow)

	// Original should be unchanged
	testifyassert.Equal(t, types.Ubps(8_000_000), input[0].BitrateHigh)
}

// --- GetDefaultAutoBitrateResolutionsConfig ---

func TestGetDefaultAutoBitrateResolutionsConfig_H264(t *testing.T) {
	config, err := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264)
	require.NoError(t, err)
	require.NotEmpty(t, config)
	// First resolution should be 4K
	testifyassert.Equal(t, uint32(3840), config[0].Resolution.Width)
	testifyassert.Equal(t, uint32(2160), config[0].Resolution.Height)
	// Last resolution should be 320x180
	testifyassert.Equal(t, uint32(320), config[len(config)-1].Resolution.Width)
}

func TestGetDefaultAutoBitrateResolutionsConfig_HEVC(t *testing.T) {
	config, err := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDHevc)
	require.NoError(t, err)
	require.NotEmpty(t, config)
	// HEVC should be 85% of H264 bitrates
	h264Config, _ := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264)
	testifyassert.Less(t, float64(config[0].BitrateHigh), float64(h264Config[0].BitrateHigh))
}

func TestGetDefaultAutoBitrateResolutionsConfig_AV1(t *testing.T) {
	config, err := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDAv1)
	require.NoError(t, err)
	require.NotEmpty(t, config)
	// AV1 always uses the maximum resolution for all bitrates: collapsed to a
	// single entry covering the union of all H264 bitrate ranges, scaled by 0.7.
	require.Len(t, config, 1)
	h264Config, _ := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264)
	testifyassert.Equal(t, h264Config[0].Resolution, config[0].Resolution)
	testifyassert.Less(t, float64(config[0].BitrateHigh), float64(h264Config[0].BitrateHigh))
	testifyassert.Less(t, float64(config[0].BitrateLow), float64(h264Config[len(h264Config)-1].BitrateLow))
}

func TestGetDefaultAutoBitrateResolutionsConfig_Unsupported(t *testing.T) {
	_, err := GetDefaultAutoBitrateResolutionsConfig(astiav.CodecID(99999))
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "unsupported codec")
}

// --- DefaultAutobitrate helpers ---

func TestDefaultAutoBitrateCalculatorThresholds(t *testing.T) {
	v := DefaultAutoBitrateCalculatorThresholds()
	require.NotNil(t, v)
}

func TestDefaultAutoBitrateCalculatorLogK(t *testing.T) {
	v := DefaultAutoBitrateCalculatorLogK()
	require.NotNil(t, v)
}

func TestDefaultAutoBitrateCalculatorQueueSizeGapDecay(t *testing.T) {
	v := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	require.NotNil(t, v)
}

// --- StreamMux creation ---

func TestNew_ForbidMode(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Equal(t, types.MuxModeForbid, s.MuxMode)
	testifyassert.Nil(t, s.InputAudioOnly)
	testifyassert.Nil(t, s.InputVideoOnly)
}

func TestNew_SameOutputSameTracks(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeSameOutputSameTracks, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Nil(t, s.InputAudioOnly)
	testifyassert.Nil(t, s.InputVideoOnly)
}

func TestNew_DifferentOutputsSameTracksSplitAV(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeDifferentOutputsSameTracksSplitAV, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	defer func() { _ = s.Close(ctx) }()

	// SplitAV mode creates separate audio and video inputs
	testifyassert.NotNil(t, s.InputAudioOnly)
	testifyassert.NotNil(t, s.InputVideoOnly)
}

// --- StreamMux basic methods ---

func TestStreamMux_GetAutoBitRateHandler_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Nil(t, s.GetAutoBitRateHandler())
}

func TestStreamMux_GetActiveVideoOutput_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	output := s.GetActiveVideoOutput(ctx)
	testifyassert.Nil(t, output)
}

func TestStreamMux_InputChan(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	ch := s.InputChan()
	testifyassert.NotNil(t, ch)
}

func TestStreamMux_OutputChan_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Nil(t, s.OutputChan())
}

func TestStreamMux_ErrorChan_Panics(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Nil(t, s.ErrorChan())
}

func TestStreamMux_GetEncoders(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	videoEnc, audioEnc := s.GetEncoders(ctx)
	testifyassert.Nil(t, videoEnc)
	testifyassert.Nil(t, audioEnc)
}

func TestStreamMux_GetVideoInput(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	input := s.GetVideoInput(ctx)
	// In Forbid mode without SplitAV, video input is the all-input
	testifyassert.NotNil(t, input)
}

func TestStreamMux_GetVideoInput_SplitAV(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeDifferentOutputsSameTracksSplitAV, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	input := s.GetVideoInput(ctx)
	testifyassert.NotNil(t, input)
	testifyassert.Equal(t, InputTypeVideoOnly, input.GetType())
}

func TestStreamMux_ForEachInput_Forbid(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	count := 0
	err = s.ForEachInput(ctx, func(ctx context.Context, input *Input[struct{}]) error {
		count++
		return nil
	})
	testifyassert.NoError(t, err)
	// Forbid mode has only InputAll
	testifyassert.Equal(t, 1, count)
}

func TestStreamMux_ForEachInput_SplitAV(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeDifferentOutputsSameTracksSplitAV, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	count := 0
	err = s.ForEachInput(ctx, func(ctx context.Context, input *Input[struct{}]) error {
		count++
		return nil
	})
	testifyassert.NoError(t, err)
	// SplitAV mode has InputAll + InputAudioOnly + InputVideoOnly = 3
	testifyassert.Equal(t, 3, count)
}

func TestStreamMux_ForEachInput_StopEarly(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeDifferentOutputsSameTracksSplitAV, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	count := 0
	err = s.ForEachInput(ctx, func(ctx context.Context, input *Input[struct{}]) error {
		count++
		return ErrStop{} // stop after first
	})
	testifyassert.NoError(t, err)
	testifyassert.Equal(t, 1, count)
}

func TestStreamMux_Close(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)

	err = s.Close(ctx)
	testifyassert.NoError(t, err)
}

// --- GetFPSFraction / SetFPSFraction ---

func TestStreamMux_FPSFraction_Default(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	r := s.GetFPSFraction(ctx)
	// Default should be 0/0 (unset) → normalized to 1/1
	_ = r
}

func TestStreamMux_SetFPSFraction(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeSameOutputSameTracks, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	s.SetFPSFraction(ctx, globaltypes.Rational{Num: 1, Den: 2})

	r := s.GetFPSFraction(ctx)
	testifyassert.Equal(t, 1, r.Num)
	testifyassert.Equal(t, 2, r.Den)
}

// --- SetAutoBitRateVideoConfig ---

func TestStreamMux_SetAutoBitRateVideoConfig_NilAlreadyDisabled(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	err = s.SetAutoBitRateVideoConfig(ctx, nil)
	testifyassert.NoError(t, err)
}

// --- IsAllowedDifferentOutputs ---

func TestStreamMux_IsAllowedDifferentOutputs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		mode     types.MuxMode
		expected bool
	}{
		{types.MuxModeForbid, false},
		{types.MuxModeSameOutputSameTracks, false},
		{types.MuxModeDifferentOutputsSameTracks, true},
		{types.MuxModeDifferentOutputsSameTracksSplitAV, true},
		{types.MuxModeSameOutputDifferentTracks, false},
	}

	for _, tt := range tests {
		t.Run(tt.mode.String(), func(t *testing.T) {
			s, err := New(ctx, tt.mode, nil)
			require.NoError(t, err)
			defer func() { _ = s.Close(ctx) }()
			testifyassert.Equal(t, tt.expected, s.IsAllowedDifferentOutputs())
		})
	}
}

// --- StreamMux node methods ---

func TestStreamMux_GetProcessor(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	proc := s.GetProcessor()
	testifyassert.NotNil(t, proc)
}

func TestStreamMux_GetCountersPtr(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	counters := s.GetCountersPtr()
	testifyassert.NotNil(t, counters)
}

func TestStreamMux_String(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Equal(t, "StreamMux", s.String())
}

func TestStreamMux_GetObjectID(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	id := s.GetObjectID()
	_ = id // should not panic
}

func TestStreamMux_IsServing_False(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.False(t, s.IsServing(ctx))
}

func TestStreamMux_OriginalNode(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	orig := s.OriginalNode()
	testifyassert.NotNil(t, orig)
	testifyassert.Equal(t, s.InputAll.Node, orig)
}

func TestStreamMux_OriginalNodeAbstract(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	origAbstract := s.OriginalNodeAbstract()
	testifyassert.NotNil(t, origAbstract)
}

func TestStreamMux_GetPushTos_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Nil(t, s.GetPushTos(ctx))
}

func TestStreamMux_RemovePushTo_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	err = s.RemovePushTo(ctx, nil)
	testifyassert.NoError(t, err)
}

func TestStreamMux_GetChangeChanPushTo_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	ch := s.GetChangeChanPushTo()
	testifyassert.Nil(t, ch)
}

func TestStreamMux_Nodes(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	nodes := s.Nodes(ctx)
	// Should have at least InputAll node
	testifyassert.GreaterOrEqual(t, len(nodes), 1)
}

func TestStreamMux_IsDrained(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// Without serving, nodes should be drained
	_ = s.IsDrained(ctx)
}

func TestStreamMux_GetChangeChanDrained(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	ch := s.GetChangeChanDrained()
	testifyassert.NotNil(t, ch)
}

func TestStreamMux_GetChangeChanIsServing(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	ch := s.GetChangeChanIsServing()
	testifyassert.NotNil(t, ch)
}

func TestStreamMux_Flush_NoOutputs(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	err = s.Flush(ctx)
	testifyassert.NoError(t, err)
}

// --- DefaultAutoBitRateVideoConfig / DefaultFPSReducerConfig ---

func TestDefaultAutoBitRateVideoConfig_H264(t *testing.T) {
	config, err := DefaultAutoBitRateVideoConfig(astiav.CodecIDH264)
	require.NoError(t, err)
	testifyassert.NotEmpty(t, config.ResolutionsAndBitRates)
	testifyassert.NotNil(t, config.Calculator)
}

func TestDefaultAutoBitRateVideoConfig_Unsupported(t *testing.T) {
	_, err := DefaultAutoBitRateVideoConfig(astiav.CodecID(99999))
	testifyassert.Error(t, err)
}

func TestDefaultFPSReducerConfig(t *testing.T) {
	config := DefaultFPSReducerConfig()
	_ = config // no panic
}

// --- streamIndexAssigner ---

func TestStreamIndexAssigner_ForbidMode(t *testing.T) {
	sia := newStreamIndexAssigner(types.MuxModeForbid, 0, nil)
	testifyassert.NotNil(t, sia)
}

func TestStreamIndexAssigner_SameOutputSameTracks(t *testing.T) {
	sia := newStreamIndexAssigner(types.MuxModeSameOutputSameTracks, 0, nil)
	testifyassert.NotNil(t, sia)
}

func TestStreamIndexAssigner_DifferentOutputsSameTracks(t *testing.T) {
	sia := newStreamIndexAssigner(types.MuxModeDifferentOutputsSameTracks, 1, nil)
	testifyassert.NotNil(t, sia)
}

// --- newTrackMeasurements ---

func TestNewTrackMeasurements(t *testing.T) {
	tm := newTrackMeasurements()
	testifyassert.NotNil(t, tm)
}

// --- getTrackMeasurements ---

func TestStreamMux_GetTrackMeasurements(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// Video measurements
	vm := s.getTrackMeasurements(astiav.MediaTypeVideo)
	testifyassert.NotNil(t, vm)

	// Audio measurements
	am := s.getTrackMeasurements(astiav.MediaTypeAudio)
	testifyassert.NotNil(t, am)

	// Unknown falls back
	um := s.getTrackMeasurements(astiav.MediaType(99))
	testifyassert.NotNil(t, um)
}

// --- GetVideoOutputIDSwitchingTo ---

func TestStreamMux_GetVideoOutputIDSwitchingTo_Nil(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	outputID := s.GetVideoOutputIDSwitchingTo(ctx)
	testifyassert.Nil(t, outputID)
}

// --- Input methods ---

func TestInput_GetType(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeDifferentOutputsSameTracksSplitAV, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Equal(t, InputTypeAll, s.InputAll.GetType())
	testifyassert.Equal(t, InputTypeAudioOnly, s.InputAudioOnly.GetType())
	testifyassert.Equal(t, InputTypeVideoOnly, s.InputVideoOnly.GetType())
}

// --- InputHandler String ---

func TestInputHandler_String(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	str := s.InputAll.Node.Processor.Kernel.Handler.String()
	testifyassert.Contains(t, str, "StreamMux:Input:all")
}

// --- CountersPtr ---

func TestStreamMux_CountersPtr(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	counters := s.CountersPtr()
	testifyassert.NotNil(t, counters)
}

// --- ForEachInput with error ---

func TestStreamMux_ForEachInput_Error(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	err = s.ForEachInput(ctx, func(ctx context.Context, input *Input[struct{}]) error {
		return errors.New("test error")
	})
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "test error")
}

// --- WithPushTos / AddPushTo / SetPushTos are no-ops ---

func TestStreamMux_WithPushTos_Noop(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// WithPushTos is a no-op — should not panic
	s.WithPushTos(ctx, nil)
}

func TestStreamMux_AddPushTo_Noop(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// Should not panic
	s.AddPushTo(ctx, nil)
}

func TestStreamMux_SetPushTos_Noop(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// Should not panic
	s.SetPushTos(ctx, nil)
}

// --- updateWithInertialValue ---

func TestUpdateWithInertialValue(t *testing.T) {
	// With low measurement count, inertia should be low (volatile)
	result := updateWithInertialValue(100, 200, 0.9, 0)
	// Should be closer to 200 since inertia is adjusted for early measurements
	testifyassert.NotEqual(t, uint64(100), result)

	// With high measurement count, inertia should be high (stable)
	result2 := updateWithInertialValue(100, 200, 0.9, 100)
	// Should still be between 100 and 200
	testifyassert.True(t, result2 >= 100 && result2 <= 200)
}

// --- NewWithCustomData ---

func TestNewWithCustomData(t *testing.T) {
	ctx := context.Background()
	type myData struct {
		Value string
	}
	s, err := NewWithCustomData[myData](ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	defer func() { _ = s.Close(ctx) }()

	testifyassert.Equal(t, types.MuxModeForbid, s.MuxMode)
}

// --- WaitForStartChan ---

func TestStreamMux_WaitForStartChan(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	ch := s.WaitForStartChan()
	testifyassert.NotNil(t, ch)
}

// --- latencyMeasurerLoop ---

// TestStreamMux_latencyMeasurerLoop_DoesNotFabricateOnMeasurementFailure
// guards against a regression where a failed measurement in
// updateSendingLatencyValues caused the loop to *fabricate* a steadily
// growing SendingLatency by adding the wall-clock tick interval each
// iteration. With no active output the measurement always errors, so a
// fresh StreamMux that runs the loop for several ticks must keep
// SendingLatency at its initial zero value for both audio and video.
func TestStreamMux_latencyMeasurerLoop_DoesNotFabricateOnMeasurementFailure(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, types.MuxModeForbid, nil)
	require.NoError(t, err)
	defer func() { _ = s.Close(ctx) }()

	// Seed both tracks so the assertion verifies "left untouched", not
	// merely "was zero". A correct implementation must preserve the
	// last-known value when the measurement cannot be refreshed.
	const seed = uint64(7_500_000) // 7.5ms
	s.getTrackMeasurements(astiav.MediaTypeVideo).SendingLatency.Store(seed)
	s.getTrackMeasurements(astiav.MediaTypeAudio).SendingLatency.Store(seed)

	// Loop ticks at 250ms; run for ~700ms (≥2 ticks) and then cancel.
	loopCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	loopErr := s.latencyMeasurerLoop(loopCtx)
	testifyassert.True(t, errors.Is(loopErr, context.DeadlineExceeded), "expected deadline-exceeded, got %v", loopErr)

	gotVideo := s.getTrackMeasurements(astiav.MediaTypeVideo).SendingLatency.Load()
	gotAudio := s.getTrackMeasurements(astiav.MediaTypeAudio).SendingLatency.Load()

	// The buggy fallback grew video.SendingLatency by ~250ms per tick on
	// measurement failure (and never touched audio). The fix removes that
	// fabrication, so both values must stay at the seed. Anything above
	// 1ms over the seed is the regression.
	testifyassert.Equal(t, seed, gotVideo, "video SendingLatency must not grow when measurement fails; got %v", time.Duration(gotVideo))
	testifyassert.Equal(t, seed, gotAudio, "audio SendingLatency must not grow when measurement fails; got %v", time.Duration(gotAudio))
}
