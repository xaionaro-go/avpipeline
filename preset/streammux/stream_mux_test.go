// stream_mux_test.go tests the stream muxer.
package streammux

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

func TestStreamMuxNodes(t *testing.T) {
	mux := &StreamMux[struct{}]{}
	v := reflect.ValueOf(mux).Elem()

	var expectedValues []node.Abstract
	for i := range v.NumField() {
		fT := v.Type().Field(i)
		fTT := fT.Type
		if !fTT.Implements(reflect.TypeOf((*node.Abstract)(nil)).Elem()) {
			continue
		}

		fV := v.Field(i)
		if fV.Interface() != nil {
			expectedValues = append(expectedValues, fV.Interface().(node.Abstract))
		}
	}

	// we have zero outputs, so there be only the global streammux nodes:
	require.Equal(t, expectedValues, mux.Nodes(context.Background()))
}

func TestStreamMuxSetResolutionBitRateCodecClearsStaleCodecNames(t *testing.T) {
	ctx := context.Background()
	mux, err := NewWithCustomData[struct{}](
		ctx,
		types.MuxModeDifferentOutputsSameTracks,
		dummyOutputFactory{},
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, mux.Close(ctx)) }()

	newRes := codec.Resolution{Width: 1280, Height: 720}
	mux.CurrentOutputProps = types.SenderProps{
		TranscoderConfig: types.TranscoderConfig{
			Output: types.TranscoderOutputConfig{
				AudioTrackConfigs: []types.OutputAudioTrackConfig{
					{
						CodecName:  "libopus",
						CodecNames: []codectypes.Name{"libopus", "aac"},
						SampleRate: 48000,
					},
				},
				VideoTrackConfigs: []types.OutputVideoTrackConfig{
					{
						CodecName:  "av1",
						CodecNames: []codectypes.Name{"av1", "libaom-av1"},
						Resolution: codec.Resolution{Width: 1920, Height: 1080},
					},
				},
			},
		},
	}

	err = mux.setResolutionBitRateCodecLocked(ctx, newRes, 4_000_000, "h264", "aac")
	require.NoError(t, err)

	cfg := mux.CurrentOutputProps.TranscoderConfig
	audioCfg := cfg.Output.AudioTrackConfigs[0]
	videoCfg := cfg.Output.VideoTrackConfigs[0]
	require.Equal(t, codectypes.Name("aac"), audioCfg.CodecName)
	require.Empty(t, audioCfg.CodecNames)
	require.Equal(t, codectypes.Name("h264"), videoCfg.CodecName)
	require.Empty(t, videoCfg.CodecNames)

	key := PartialSenderKeyFromTranscoderConfig(ctx, &cfg)
	require.Equal(t, codectypes.Name("aac"), key.AudioCodec)
	require.NotEqual(t, codectypes.Name("libopus"), key.AudioCodec)
	require.Equal(t, codectypes.Name("h264"), key.VideoCodec)
	require.NotEqual(t, codectypes.Name("av1"), key.VideoCodec)
	require.Equal(t, newRes, key.VideoResolution)
}

func TestSetPreferredOutputsSplitAVRejectsIncompletePlanWithoutPartialSwitch(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const originalVideoID OutputID = 7
	const nextVideoID OutputID = 11

	videoKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1280, Height: 720},
	}
	audioKey := SenderKey{
		AudioCodec:      codectypes.Name("aac"),
		AudioSampleRate: audio.SampleRate(48_000),
	}
	combinedKey := SenderKey{
		VideoCodec:      videoKey.VideoCodec,
		VideoResolution: videoKey.VideoResolution,
		AudioCodec:      audioKey.AudioCodec,
		AudioSampleRate: audioKey.AudioSampleRate,
	}
	videoOutput := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, nextVideoID, videoKey)
	mux.Outputs.Store(nextVideoID, videoOutput)
	mux.OutputsMap.Store(videoOutput.StorageKey(), videoOutput)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(originalVideoID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(originalVideoID))

	err := mux.setPreferredOutputs(ctx, combinedKey)

	require.Error(t, err)
	require.Equal(t, int32(originalVideoID), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"invalid SplitAV route plans must be rejected before switching any route")
	require.Equal(t, int32(originalVideoID), mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"invalid SplitAV route plans must not sync a partially switched route")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.NextValue.Load(),
		"invalid SplitAV route plans must not leave a pending partial switch")
}
