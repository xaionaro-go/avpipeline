package transcoderwithpassthrough

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestInitTranscoderUsesInputDecoderAndOutputEncoderCandidateConfig(t *testing.T) {
	ctx := context.Background()
	s := &TranscoderWithPassthrough[struct{}, *processor.Dummy]{}

	cfg := types.TranscoderConfig{
		Input: &types.TranscoderInputConfig{
			AudioTrackConfigs: []types.InputAudioTrackConfig{
				{
					CodecNames: []codectypes.Name{"aac", "mp3"},
				},
			},
			VideoTrackConfigs: []types.InputVideoTrackConfig{
				{
					CodecNames:         []codectypes.Name{"av1_cuvid", "libdav1d", "av1"},
					CustomOptions:      types.DictionaryItems{{Key: "strict", Value: "experimental"}},
					HardwareDeviceType: types.HardwareDeviceTypeCUDA,
					HardwareDeviceName: types.HardwareDeviceName("decoder-gpu"),
				},
			},
		},
		Output: types.TranscoderOutputConfig{
			AudioTrackConfigs: []types.AudioTrackConfig{
				{
					CodecNames: []codectypes.Name{"aac", "libopus"},
				},
			},
			VideoTrackConfigs: []types.VideoTrackConfig{
				{
					CodecNames:         []codectypes.Name{"h264_nvenc", "libx264"},
					CustomOptions:      types.DictionaryItems{{Key: "preset", Value: "fast"}},
					HardwareDeviceType: types.HardwareDeviceTypeCUDA,
					HardwareDeviceName: types.HardwareDeviceName("encoder-gpu"),
					Resolution:         codec.Resolution{Width: 1920, Height: 1080},
				},
			},
		},
	}

	require.NoError(t, s.initTranscoder(ctx, cfg))
	require.NotNil(t, s.Transcoder)

	decoderFactory := s.Transcoder.DecoderFactory
	assert.Equal(t, []codec.Name{"av1_cuvid", "libdav1d", "av1"}, decoderFactory.VideoCodecs)
	assert.Equal(t, []codec.Name{"aac", "mp3"}, decoderFactory.AudioCodecs)
	assert.Equal(t, globaltypes.HardwareDeviceTypeCUDA, globaltypes.HardwareDeviceType(decoderFactory.HardwareDeviceType))
	assert.Equal(t, codec.HardwareDeviceName("decoder-gpu"), decoderFactory.HardwareDeviceName)
	require.NotNil(t, decoderFactory.VideoOptions)
	require.NotNil(t, decoderFactory.VideoOptions.Get("strict", nil, 0))
	assert.Equal(t, "experimental", decoderFactory.VideoOptions.Get("strict", nil, 0).Value())

	encoderFactory := s.Transcoder.EncoderFactory
	assert.Equal(t, []codec.Name{"h264_nvenc", "libx264"}, encoderFactory.VideoCodecs)
	assert.Equal(t, []codec.Name{"aac", "libopus"}, encoderFactory.AudioCodecs)
	assert.Equal(t, globaltypes.HardwareDeviceTypeCUDA, globaltypes.HardwareDeviceType(encoderFactory.HardwareDeviceType))
	assert.Equal(t, codec.HardwareDeviceName("encoder-gpu"), encoderFactory.HardwareDeviceName)
	require.NotNil(t, encoderFactory.VideoOptions)
	require.NotNil(t, encoderFactory.VideoOptions.Get("preset", nil, 0))
	assert.Equal(t, "fast", encoderFactory.VideoOptions.Get("preset", nil, 0).Value())
}
