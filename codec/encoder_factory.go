package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type EncoderFactory interface {
	fmt.Stringer
	NewEncoder(ctx context.Context, params *astiav.CodecParameters, timeBase astiav.Rational) (Encoder, error)
}

type NaiveEncoderFactory struct {
	VideoCodec         string
	AudioCodec         string
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	VideoOptions       *astiav.Dictionary
	AudioOptions       *astiav.Dictionary
	VideoEncoders      []Encoder
	AudioEncoders      []Encoder
	Locker             xsync.Mutex
}

var _ EncoderFactory = (*NaiveEncoderFactory)(nil)

func NewNaiveEncoderFactory(
	ctx context.Context,
	videoCodec string,
	audioCodec string,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	videoCustomOptions types.DictionaryItems,
	audioCustomOptions types.DictionaryItems,
) *NaiveEncoderFactory {
	f := &NaiveEncoderFactory{
		VideoCodec:         videoCodec,
		AudioCodec:         audioCodec,
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
	}
	if len(videoCustomOptions) > 0 {
		f.VideoOptions = astiav.NewDictionary()
		setFinalizerFree(ctx, f.VideoOptions)

		for _, opt := range videoCustomOptions {
			logger.Debugf(ctx, "encoderFactory.VideoOptions['%s'] = '%s'", opt.Key, opt.Value)
			f.VideoOptions.Set(opt.Key, opt.Value, 0)
		}
	}
	if len(audioCustomOptions) > 0 {
		f.AudioOptions = astiav.NewDictionary()
		setFinalizerFree(ctx, f.AudioOptions)

		for _, opt := range audioCustomOptions {
			logger.Debugf(ctx, "encoderFactory.AudioOptions['%s'] = '%s'", opt.Key, opt.Value)
			f.AudioOptions.Set(opt.Key, opt.Value, 0)
		}
	}
	return f
}

func (f *NaiveEncoderFactory) String() string {
	return fmt.Sprintf("NaiveEncoderFactory(%s/%s)", f.VideoCodec, f.AudioCodec)
}

func (f *NaiveEncoderFactory) VideoCodecID() astiav.CodecID {
	if f.VideoCodec == CodecNameCopy {
		return 0
	}
	return findEncoderCodec(0, f.VideoCodec).ID()
}

func (f *NaiveEncoderFactory) AudioCodecID() astiav.CodecID {
	if f.AudioCodec == CodecNameCopy {
		return 0
	}
	return findEncoderCodec(0, f.AudioCodec).ID()
}

func (f *NaiveEncoderFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
) (_ret Encoder, _err error) {
	return xsync.DoA3R2(xsync.WithNoLogging(ctx, true), &f.Locker, f.newEncoderNoLock, ctx, params, timeBase)
}

func (f *NaiveEncoderFactory) newEncoderNoLock(
	ctx context.Context,
	codecParamsOrig *astiav.CodecParameters,
	timeBase astiav.Rational,
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

	var encParams *EncoderParams
	switch codecParams.MediaType() {
	case astiav.MediaTypeVideo:
		encParams = &EncoderParams{
			CodecName:          f.VideoCodec,
			CodecParameters:    codecParams,
			HardwareDeviceType: f.HardwareDeviceType,
			HardwareDeviceName: f.HardwareDeviceName,
			TimeBase:           timeBase,
			Options:            f.VideoOptions,
		}
	case astiav.MediaTypeAudio:
		encParams = &EncoderParams{
			CodecName:       f.AudioCodec,
			CodecParameters: codecParams,
			TimeBase:        timeBase,
			Options:         f.VideoOptions,
		}
	default:
		return nil, fmt.Errorf("only audio and video tracks are supported by NaiveEncoderFactory, yet")
	}
	return NewEncoder(ctx, *encParams)
}
