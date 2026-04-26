// encoder_factory.go defines the EncoderFactory interface and its naive implementation.

package codec

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type EncoderFactory interface {
	fmt.Stringer
	NewEncoder(
		ctx context.Context,
		params *astiav.CodecParameters,
		timeBase astiav.Rational,
		opts ...Option,
	) (Encoder, error)
	Reset(ctx context.Context) error
}

type ResourcesGetter = resource.ResourcesGetter

type NaiveEncoderFactory struct {
	NaiveEncoderFactoryParams

	Locker          xsync.Mutex
	VideoEncoders   []Encoder
	AudioEncoders   []Encoder
	ResourceManager ResourceManager
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
	AudioQuality          Quality
	AudioSampleRate       audio.SampleRate
	AudioChannels         audio.Channel
	Options               []Option
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
	codec := findEncoderCodec(0, f.VideoCodec)
	if codec == nil {
		return 0
	}
	return codec.ID()
}

func (f *NaiveEncoderFactory) AudioCodecID() astiav.CodecID {
	if f.AudioCodec == NameCopy {
		return 0
	}
	codec := findEncoderCodec(0, f.AudioCodec)
	if codec == nil {
		return 0
	}
	return codec.ID()
}

type GetDecoderer interface {
	GetDecoder() *Decoder
}

func (f *NaiveEncoderFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...Option,
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
	opts ...Option,
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

	optsCombined := make([]Option, 0, len(f.Options)+len(opts))
	optsCombined = append(optsCombined, f.Options...)
	optsCombined = append(optsCombined, opts...)

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
			CustomOptions:      f.VideoOptions,
			ResourceManager:    f.ResourceManager,
			Options:            optsCombined,
		}
	case astiav.MediaTypeAudio:
		if err := f.amendAudioCodecParams(ctx, codecParams); err != nil {
			return nil, fmt.Errorf("unable to amend audio codec parameters: %w", err)
		}
		encParams = &CodecParams{
			CodecName:       f.AudioCodec,
			CodecParameters: codecParams,
			TimeBase:        timeBase,
			CustomOptions:   f.AudioOptions,
			ResourceManager: f.ResourceManager,
			Options:         optsCombined,
		}
	default:
		// Non-AV streams (subtitles, data, attachments) are passed through as-is.
		// EncoderCopy is stateless; the transcoder's existing copy path will then
		// call initOutputStreamCopy and emit the cloned packet on outputCh.
		return EncoderCopy{}, nil
	}

	return NewEncoder(ctx, *encParams)
}

func (f *NaiveEncoderFactory) Reset(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &f.Locker, f.reset, ctx)
}

func (f *NaiveEncoderFactory) reset(
	ctx context.Context,
) error {
	var errs []error
	f.VideoEncoders = nil
	f.AudioEncoders = nil
	return errors.Join(errs...)
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
		targetW, targetH := int(f.VideoResolution.Width), int(f.VideoResolution.Height)
		frameW, frameH := codecParams.Width(), codecParams.Height()

		// When autorotate applies 90°/270° rotation, the decoded frame dimensions
		// are the transpose of the configured resolution. Use the post-rotation
		// dimensions so the encoder matches what the decoder actually produces.
		if frameW == targetH && frameH == targetW && targetW != targetH {
			logger.Debugf(ctx, "frame dimensions %dx%d are rotated from configured %dx%d; using post-rotation dimensions",
				frameW, frameH, targetW, targetH)
			f.VideoResolution.Width = uint32(frameW)
			f.VideoResolution.Height = uint32(frameH)
		}

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

// amendAudioCodecParams applies configured audio parameters (quality, sample
// rate, channel layout) to codecParams so the encoder uses the requested
// values instead of the input stream's defaults.
func (f *NaiveEncoderFactory) amendAudioCodecParams(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
) (_err error) {
	logger.Tracef(ctx, "amendAudioCodecParams")
	defer func() { logger.Tracef(ctx, "/amendAudioCodecParams: %v", _err) }()

	var errs []error
	if f.AudioQuality != nil {
		logger.Tracef(ctx, "applying audio quality %v", f.AudioQuality)
		if err := f.AudioQuality.Apply(codecParams); err != nil {
			errs = append(errs, fmt.Errorf("unable to apply audio quality %#+v: %w", f.AudioQuality, err))
		}
	}

	if f.AudioSampleRate > 0 {
		logger.Tracef(ctx, "applying audio sample rate %d", f.AudioSampleRate)
		codecParams.SetSampleRate(int(f.AudioSampleRate))
	}

	if f.AudioChannels > 0 {
		logger.Tracef(ctx, "applying audio channels %d", f.AudioChannels)
		layout, err := channelLayoutFromCount(f.AudioChannels)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to apply audio channels %d: %w", f.AudioChannels, err))
		} else {
			codecParams.SetChannelLayout(layout)
		}
	}

	return errors.Join(errs...)
}
