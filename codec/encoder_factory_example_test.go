package codec_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/quality"
)

type metadataEncoderFactory struct {
	videoCodec codec.Name
	audioCodec codec.Name
}

var _ codec.EncoderFactory = (*metadataEncoderFactory)(nil)

func (f *metadataEncoderFactory) String() string {
	return fmt.Sprintf("metadataEncoderFactory(%s/%s)", f.videoCodec, f.audioCodec)
}

func (f *metadataEncoderFactory) Reset(ctx context.Context) error {
	return nil
}

func (f *metadataEncoderFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codec.Option,
) (codec.Encoder, error) {
	switch {
	case params == nil:
		return nil, errors.New("nil codec parameters")
	case timeBase.Num() == 0 || timeBase.Den() == 0:
		return nil, fmt.Errorf("zero time base %s", timeBase)
	}

	delegateParams := &codec.NaiveEncoderFactoryParams{}
	var videoOptions *astiav.Dictionary
	defer func() {
		if videoOptions != nil {
			videoOptions.Free()
		}
	}()

	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		delegateParams.VideoCodec = f.videoCodec
		delegateParams.VideoQuality = quality.ConstantBitrate(videoBitrateFor(params))
		delegateParams.VideoResolution = capVideoResolution(params, 1920)
		videoOptions = astiav.NewDictionary()
		if err := videoOptions.Set("preset", "veryfast", 0); err != nil {
			return nil, fmt.Errorf("unable to set video option: %w", err)
		}
		delegateParams.VideoOptions = videoOptions
	case astiav.MediaTypeAudio:
		delegateParams.AudioCodec = f.audioCodec
		delegateParams.AudioQuality = quality.ConstantBitrate(audioBitrateFor(params))
	case astiav.MediaTypeSubtitle, astiav.MediaTypeData, astiav.MediaTypeAttachment:
		return codec.EncoderCopy{}, nil
	default:
		return nil, fmt.Errorf("unsupported media type %s", params.MediaType())
	}

	if getDecoderer, ok := codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](opts); ok {
		decoder := getDecoderer.GetDecoder()
		if decoder != nil {
			_ = decoder.IsDirty(ctx)
		}
	}

	delegate := codec.NewNaiveEncoderFactory(ctx, delegateParams)
	return delegate.NewEncoder(ctx, params, timeBase, opts...)
}

func videoBitrateFor(params *astiav.CodecParameters) uint {
	pixels := params.Width() * params.Height()
	fps := 30
	frameRate := params.FrameRate()
	if frameRate.Num() > 0 && frameRate.Den() > 0 {
		fps = frameRate.Num() / frameRate.Den()
	}
	switch {
	case pixels >= 3840*2160 || fps > 50:
		return 8_000_000
	case pixels >= 1920*1080:
		return 4_500_000
	case pixels >= 1280*720:
		return 2_500_000
	default:
		return 1_200_000
	}
}

func audioBitrateFor(params *astiav.CodecParameters) uint {
	channels := params.ChannelLayout().Channels()
	switch {
	case channels >= 6:
		return 384_000
	case channels == 2:
		return 160_000
	default:
		return 96_000
	}
}

func capVideoResolution(
	params *astiav.CodecParameters,
	maxWidth int,
) *codec.Resolution {
	width := params.Width()
	height := params.Height()
	switch {
	case width <= 0 || height <= 0:
		return nil
	case width <= maxWidth:
		return nil
	}

	scaledHeight := height * maxWidth / width
	if scaledHeight%2 != 0 {
		scaledHeight--
	}
	if scaledHeight <= 0 {
		scaledHeight = 2
	}
	return &codec.Resolution{
		Width:  uint32(maxWidth),
		Height: uint32(scaledHeight),
	}
}

func ExampleEncoderFactory_customImplementation() {
	ctx := context.Background()
	encoderFactory := &metadataEncoderFactory{
		videoCodec: codec.Name("libx264"),
		audioCodec: codec.Name("aac"),
	}

	transcoder, err := kernel.NewTranscoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		encoderFactory,
		nil,
	)
	if err != nil {
		panic(err)
	}
	defer transcoder.Close(ctx)
}

func ExampleEncoderFactory_streamMetadata() {
	ctx := context.Background()
	params := astiav.AllocCodecParameters()
	defer params.Free()
	params.SetMediaType(astiav.MediaTypeVideo)
	params.SetCodecID(astiav.CodecIDH264)
	params.SetWidth(3840)
	params.SetHeight(2160)
	params.SetFrameRate(astiav.NewRational(60, 1))

	encoderFactory := &metadataEncoderFactory{
		videoCodec: codec.Name("libx264"),
		audioCodec: codec.Name("aac"),
	}
	_, err := encoderFactory.NewEncoder(ctx, params, astiav.NewRational(1, 60))
	if err != nil {
		panic(err)
	}
}
