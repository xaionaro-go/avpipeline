// stream_mux_test.go tests the stream muxer.
package streammux

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
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
