package codec

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/resourcegetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type EncoderFactory interface {
	fmt.Stringer
	NewEncoder(
		ctx context.Context,
		params *astiav.CodecParameters,
		timeBase astiav.Rational,
		opts ...EncoderFactoryOption,
	) (Encoder, error)
}

type ResourcesGetter = resourcegetter.ResourcesGetter

type NaiveEncoderFactory struct {
	NaiveEncoderFactoryParams

	Locker          xsync.Mutex
	VideoEncoders   []Encoder
	AudioEncoders   []Encoder
	ResourcesGetter ResourcesGetter
}

type NaiveEncoderFactoryParams struct {
	VideoCodec            Name
	AudioCodec            Name
	HardwareDeviceType    HardwareDeviceType
	HardwareDeviceName    HardwareDeviceName
	VideoOptions          *astiav.Dictionary
	AudioOptions          *astiav.Dictionary
	VideoQuality          Quality
	VideoResolution       *Resolution
	VideoAverageFrameRate astiav.Rational
}

func DefaultNaiveEncoderFactoryParams() *NaiveEncoderFactoryParams {
	return &NaiveEncoderFactoryParams{}
}

var _ EncoderFactory = (*NaiveEncoderFactory)(nil)

func NewNaiveEncoderFactory(
	ctx context.Context,
	params *NaiveEncoderFactoryParams,
) *NaiveEncoderFactory {
	if params == nil {
		params = DefaultNaiveEncoderFactoryParams()
	}
	return &NaiveEncoderFactory{
		NaiveEncoderFactoryParams: *params,
	}
}

func (f *NaiveEncoderFactory) String() string {
	return fmt.Sprintf("NaiveEncoderFactory(%s/%s)", f.VideoCodec, f.AudioCodec)
}

func (f *NaiveEncoderFactory) VideoCodecID() astiav.CodecID {
	if f.VideoCodec == NameCopy {
		return 0
	}
	return findEncoderCodec(0, f.VideoCodec).ID()
}

func (f *NaiveEncoderFactory) AudioCodecID() astiav.CodecID {
	if f.AudioCodec == NameCopy {
		return 0
	}
	return findEncoderCodec(0, f.AudioCodec).ID()
}

type FrameSource interface {
	GetDecoder() *Decoder
}

func (f *NaiveEncoderFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...EncoderFactoryOption,
) (_ret Encoder, _err error) {
	logger.Tracef(ctx, "NewEncoder: %#+v, %s", params, timeBase)
	defer func() { logger.Tracef(ctx, "/NewEncoder: %#+v, %s: %T %v", params, timeBase, _ret, _err) }()
	return xsync.DoR2(xsync.WithNoLogging(ctx, true), &f.Locker, func() (Encoder, error) {
		return f.newEncoderLocked(ctx, params, timeBase, opts...)
	})
}

func (f *NaiveEncoderFactory) newEncoderLocked(
	ctx context.Context,
	codecParamsOrig *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...EncoderFactoryOption,
) (_ret Encoder, _err error) {
	if timeBase.Num() == 0 {
		return nil, fmt.Errorf("TimeBase must be set")
	}
	codecParams := astiav.AllocCodecParameters()
	setFinalizerFree(ctx, codecParams)
	codecParamsOrig.Copy(codecParams)

	defer func() {
		if _err != nil {
			return
		}
		switch codecParams.MediaType() {
		case astiav.MediaTypeVideo:
			f.VideoEncoders = append(f.VideoEncoders, _ret)
		case astiav.MediaTypeAudio:
			f.AudioEncoders = append(f.AudioEncoders, _ret)
		}
	}()

	var encParams *CodecParams
	switch codecParams.MediaType() {
	case astiav.MediaTypeVideo:
		if err := f.amendVideoCodecParams(ctx, codecParams); err != nil {
			return nil, fmt.Errorf("unable to amend video codec parameters: %w", err)
		}
		encParams = &CodecParams{
			CodecName:          f.VideoCodec,
			CodecParameters:    codecParams,
			HardwareDeviceType: f.HardwareDeviceType,
			HardwareDeviceName: f.HardwareDeviceName,
			TimeBase:           timeBase,
			Options:            f.VideoOptions,
		}
	case astiav.MediaTypeAudio:
		encParams = &CodecParams{
			CodecName:       f.AudioCodec,
			CodecParameters: codecParams,
			TimeBase:        timeBase,
			Options:         f.AudioOptions,
		}
	default:
		return nil, fmt.Errorf("only audio and video tracks are supported by NaiveEncoderFactory, yet")
	}

	if f.ResourcesGetter != nil {
		logger.Tracef(ctx, "getting reusable resources from %s", f.ResourcesGetter)
		reusableResources := f.ResourcesGetter.GetResources(ctx, codecParams, timeBase, opts...)
		if reusableResources != nil {
			opts = append(opts, EncoderFactoryOptionReusableResources{Resources: reusableResources})
		}
	}
	return NewEncoder(ctx, *encParams, opts...)
}

func (f *NaiveEncoderFactory) amendVideoCodecParams(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
) (_err error) {
	logger.Tracef(ctx, "amendVideoCodecParams")
	defer func() { logger.Tracef(ctx, "/amendVideoCodecParams: %v", _err) }()

	var errs []error
	if f.VideoQuality != nil {
		logger.Tracef(ctx, "applying video quality %v", f.VideoQuality)
		if err := f.VideoQuality.Apply(codecParams); err != nil {
			errs = append(errs, fmt.Errorf("unable to apply video quality %#+v: %w", f.VideoQuality, err))
		}
	}
	if f.VideoResolution != nil {
		logger.Tracef(ctx, "applying video resolution %#+v", f.VideoResolution)
		codecParams.SetWidth(int(f.VideoResolution.Width))
		codecParams.SetHeight(int(f.VideoResolution.Height))
	}
	if f.VideoAverageFrameRate.Num() > 0 {
		logger.Tracef(ctx, "applying video average frame rate %s", f.VideoAverageFrameRate)
		codecParams.SetFrameRate(f.VideoAverageFrameRate)
	}
	return errors.Join(errs...)
}
