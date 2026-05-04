// output_test.go tests the output of the stream muxer.
package streammux

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

type dummyHandler struct {
	boilerplate.CustomHandler
}

func (dummyHandler) String() string {
	return "dummyHandler"
}

type dummyOutputFactory struct{}

func (dummyOutputFactory) NewSender(
	ctx context.Context,
	outputKey SenderKey,
) (SendingNode[struct{}], types.SenderConfig, error) {
	return node.NewWithCustomDataFromKernel[OutputCustomData[struct{}]](
		ctx,
		boilerplate.NewKernelWithFormatContext(ctx, &dummyHandler{}),
	), types.SenderConfig{}, nil
}

func TestOutputNodes(t *testing.T) {
	ctx := context.Background()
	input, err := newInput[struct{}](ctx, nil, InputTypeAll)
	require.NoError(t, err)
	output, err := newOutput[struct{}](
		ctx,
		1,
		input.Node,
		dummyOutputFactory{},
		SenderKey{
			VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		},
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

	v := reflect.ValueOf(output).Elem()

	var expectedValues []node.Abstract
	for i := range v.NumField() {
		fT := v.Type().Field(i)
		if fT.Name == "InputFrom" {
			continue
		}
		fTT := fT.Type
		if !fTT.Implements(reflect.TypeOf((*node.Abstract)(nil)).Elem()) {
			continue
		}

		fV := v.Field(i)
		expectedValues = append(expectedValues, fV.Interface().(node.Abstract))
	}

	// we have zero outputs, so there be only the global streammux nodes:
	require.Equal(t, expectedValues, output.Nodes())
}

func TestPartialSenderKeyFromTranscoderConfigUsesCodecNamesFirstCandidate(t *testing.T) {
	ctx := context.Background()
	cfg := &types.TranscoderConfig{
		Output: types.TranscoderOutputConfig{
			AudioTrackConfigs: []types.OutputAudioTrackConfig{
				{
					CodecName:  "aac",
					CodecNames: []codectypes.Name{"mp3", "aac"},
				},
			},
			VideoTrackConfigs: []types.OutputVideoTrackConfig{
				{
					CodecName:  "mpeg4",
					CodecNames: []codectypes.Name{"h264", "libx264"},
					Resolution: codectypes.Resolution{Width: 1920, Height: 1080},
				},
			},
		},
	}

	key := PartialSenderKeyFromTranscoderConfig(ctx, cfg)

	require.Equal(t, codectypes.Name("mp3"), key.AudioCodec)
	require.Equal(t, codectypes.Name("h264"), key.VideoCodec)
	require.Equal(t, codectypes.Resolution{Width: 1920, Height: 1080}, key.VideoResolution)
}

func TestOutputReconfigureEncoderUsesCodecNames(t *testing.T) {
	ctx := context.Background()
	output := newOutputWithEncoderFactory(ctx, codec.NewNaiveEncoderFactory(ctx, nil))
	cfg := types.TranscoderConfig{
		Output: types.TranscoderOutputConfig{
			AudioTrackConfigs: []types.OutputAudioTrackConfig{
				{
					CodecName:  "aac",
					CodecNames: []codectypes.Name{"mp3", "aac"},
				},
			},
			VideoTrackConfigs: []types.OutputVideoTrackConfig{
				{
					CodecName:  "mpeg4",
					CodecNames: []codectypes.Name{"h264", "libx264"},
					Resolution: codectypes.Resolution{Width: 1920, Height: 1080},
				},
			},
		},
	}

	_, err := output.reconfigureEncoder(ctx, cfg)
	require.NoError(t, err)

	encoderFactory := output.TranscoderNode.Processor.Kernel.EncoderFactory
	require.Equal(t, codec.Name("h264"), encoderFactory.VideoCodec)
	require.Equal(t, []codec.Name{"h264", "libx264"}, encoderFactory.VideoCodecs)
	require.Equal(t, codec.Name("mp3"), encoderFactory.AudioCodec)
	require.Equal(t, []codec.Name{"mp3", "aac"}, encoderFactory.AudioCodecs)
}

func TestOutputReconfigureDecoderUsesInputCodecNames(t *testing.T) {
	ctx := context.Background()
	decoderFactory := codec.NewNaiveDecoderFactory(ctx, nil)
	output := newOutputWithFactories(ctx, decoderFactory, codec.NewNaiveEncoderFactory(ctx, nil))
	cfg := types.TranscoderConfig{
		Input: &types.TranscoderInputConfig{
			VideoTrackConfigs: []types.InputVideoTrackConfig{
				{
					CodecName:  "av1",
					CodecNames: []codectypes.Name{"av1_cuvid", "libdav1d", "av1"},
				},
			},
		},
		Output: types.TranscoderOutputConfig{
			VideoTrackConfigs: []types.OutputVideoTrackConfig{{CodecName: "h264"}},
		},
	}

	err := output.reconfigureDecoder(ctx, cfg)
	require.NoError(t, err)

	require.Equal(t, codec.Name("av1_cuvid"), decoderFactory.VideoCodec)
	require.Equal(t, []codec.Name{"av1_cuvid", "libdav1d", "av1"}, decoderFactory.VideoCodecs)
}

func TestOutputReconfigureTranscoderCodecNamesOverrideScalarCopy(t *testing.T) {
	ctx := context.Background()
	decoderFactory := codec.NewNaiveDecoderFactory(ctx, nil)
	encoderFactory := codec.NewNaiveEncoderFactory(ctx, nil)
	output := newOutputWithFactories(ctx, decoderFactory, encoderFactory)
	input, err := newInput[struct{}](ctx, nil, InputTypeAll)
	require.NoError(t, err)
	output.InputFrom = input.Node
	cfg := types.TranscoderConfig{
		Input: &types.TranscoderInputConfig{
			VideoTrackConfigs: []types.InputVideoTrackConfig{
				{
					CodecName:  "av1",
					CodecNames: []codectypes.Name{"av1_cuvid", "libdav1d", "av1"},
				},
			},
		},
		Output: types.TranscoderOutputConfig{
			VideoTrackConfigs: []types.OutputVideoTrackConfig{
				{
					CodecName:  codectypes.Name(codec.NameCopy),
					CodecNames: []codectypes.Name{"h264", "libx264"},
					Resolution: codectypes.Resolution{Width: 1920, Height: 1080},
				},
			},
		},
	}

	err = output.reconfigureTranscoder(ctx, cfg)
	require.NoError(t, err)

	require.Equal(t, codec.Name("h264"), encoderFactory.VideoCodec)
	require.NotEqual(t, codec.Name(codec.NameCopy), encoderFactory.VideoCodec)
	require.Equal(t, []codec.Name{"h264", "libx264"}, encoderFactory.VideoCodecs)
	require.Equal(t, codec.Name("av1_cuvid"), decoderFactory.VideoCodec)
	require.Equal(t, []codec.Name{"av1_cuvid", "libdav1d", "av1"}, decoderFactory.VideoCodecs)
}

func newOutputWithEncoderFactory(
	ctx context.Context,
	encoderFactory *codec.NaiveEncoderFactory,
) *Output[struct{}] {
	return newOutputWithFactories(ctx, codec.NewNaiveDecoderFactory(ctx, nil), encoderFactory)
}

func newOutputWithFactories(
	ctx context.Context,
	decoderFactory *codec.NaiveDecoderFactory,
	encoderFactory *codec.NaiveEncoderFactory,
) *Output[struct{}] {
	return &Output[struct{}]{
		TranscoderNode: &NodeTranscoder[OutputCustomData[struct{}]]{
			Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
				Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
					Decoder: kernel.NewDecoder(ctx, decoderFactory),
					Encoder: kernel.NewEncoder(ctx, encoderFactory, nil),
				},
			},
		},
	}
}
