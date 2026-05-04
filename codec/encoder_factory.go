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
	globaltypes "github.com/xaionaro-go/avpipeline/types"
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
	VideoCodec         Name
	VideoCodecs        []Name
	AudioCodec         Name
	AudioCodecs        []Name
	HardwareDeviceType HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	// VideoOptions and AudioOptions are shared configuration and are cloned
	// for each candidate attempt. Keep them limited to options accepted by
	// every candidate in the corresponding priority list.
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
	if len(f.VideoCodecs) == 0 && len(f.AudioCodecs) == 0 {
		return fmt.Sprintf("NaiveEncoderFactory(%s/%s)", f.VideoCodec, f.AudioCodec)
	}
	return fmt.Sprintf("NaiveEncoderFactory(%v:%s/%v:%s)", f.VideoCodecs, f.VideoCodec, f.AudioCodecs, f.AudioCodec)
}

func (f *NaiveEncoderFactory) VideoCodecID() astiav.CodecID {
	videoCodec := f.primaryVideoCodec()
	if videoCodec == NameCopy {
		return 0
	}
	codec := findEncoderCodec(0, videoCodec)
	if codec == nil {
		return 0
	}
	return codec.ID()
}

func (f *NaiveEncoderFactory) AudioCodecID() astiav.CodecID {
	audioCodec := f.primaryAudioCodec()
	if audioCodec == NameCopy {
		return 0
	}
	codec := findEncoderCodec(0, audioCodec)
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
	mediaType := codecParamsOrig.MediaType()

	defer func() {
		if _err != nil {
			return
		}
		switch mediaType {
		case astiav.MediaTypeVideo:
			f.VideoEncoders = append(f.VideoEncoders, _ret)
		case astiav.MediaTypeAudio:
			f.AudioEncoders = append(f.AudioEncoders, _ret)
		}
	}()

	optsCombined := make([]Option, 0, len(f.Options)+len(opts))
	optsCombined = append(optsCombined, f.Options...)
	optsCombined = append(optsCombined, opts...)

	var candidates []codecCandidate
	var err error
	switch mediaType {
	case astiav.MediaTypeVideo:
		candidates, err = f.videoEncoderCandidates(ctx)
		if err != nil {
			return nil, err
		}
	case astiav.MediaTypeAudio:
		candidates, err = f.audioEncoderCandidates(ctx)
		if err != nil {
			return nil, err
		}
	default:
		// Non-AV streams (subtitles, data, attachments) are passed through as-is.
		// EncoderCopy is stateless; the transcoder's existing copy path will then
		// call initOutputStreamCopy and emit the cloned packet on outputCh.
		return EncoderCopy{}, nil
	}
	if err := validateEncoderCandidateCodecIDs(ctx, mediaType, candidates); err != nil {
		return nil, err
	}

	var errs []error
	for _, candidate := range candidates {
		codecParams := astiav.AllocCodecParameters()
		setFinalizerFree(ctx, codecParams)
		codecParamsOrig.Copy(codecParams)

		switch mediaType {
		case astiav.MediaTypeVideo:
			if err := f.amendVideoCodecParams(ctx, codecParams); err != nil {
				return nil, fmt.Errorf("unable to amend video codec parameters: %w", err)
			}
		case astiav.MediaTypeAudio:
			if err := f.amendAudioCodecParams(ctx, codecParams); err != nil {
				return nil, fmt.Errorf("unable to amend audio codec parameters: %w", err)
			}
		}

		if err := validateCodecCandidate(ctx, true, candidate, codecParams.CodecID()); err != nil {
			errs = append(errs, err)
			continue
		}

		enc, err := NewEncoder(ctx, CodecParams{
			CodecName:          candidate.CodecName,
			CodecParameters:    codecParams,
			HardwareDeviceType: candidate.HardwareDeviceType,
			HardwareDeviceName: candidate.HardwareDeviceName,
			TimeBase:           timeBase,
			CustomOptions:      candidate.CustomOptions,
			ResourceManager:    f.ResourceManager,
			Options:            optsCombined,
		})
		if err == nil {
			return enc, nil
		}
		if !isRetryableCodecCandidateError(err) {
			return nil, err
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func (f *NaiveEncoderFactory) primaryVideoCodec() Name {
	if len(f.VideoCodecs) > 0 {
		return f.VideoCodecs[0]
	}
	return f.VideoCodec
}

func (f *NaiveEncoderFactory) primaryAudioCodec() Name {
	if len(f.AudioCodecs) > 0 {
		return f.AudioCodecs[0]
	}
	return f.AudioCodec
}

func (f *NaiveEncoderFactory) audioEncoderCandidates(
	ctx context.Context,
) ([]codecCandidate, error) {
	if len(f.AudioCodecs) == 0 {
		candidate, err := newCodecCandidate(ctx, f.AudioCodec, 0, "", f.AudioOptions, false)
		if err != nil {
			return nil, err
		}
		return []codecCandidate{candidate}, nil
	}
	result := make([]codecCandidate, 0, len(f.AudioCodecs))
	for _, codecName := range f.AudioCodecs {
		candidate, err := newCodecCandidate(ctx, codecName, 0, "", f.AudioOptions, true)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (f *NaiveEncoderFactory) videoEncoderCandidates(
	ctx context.Context,
) ([]codecCandidate, error) {
	if len(f.VideoCodecs) == 0 {
		candidate, err := newCodecCandidate(ctx, f.VideoCodec, f.HardwareDeviceType, f.HardwareDeviceName, f.VideoOptions, false)
		if err != nil {
			return nil, err
		}
		return []codecCandidate{candidate}, nil
	}
	result := make([]codecCandidate, 0, len(f.VideoCodecs))
	for _, codecName := range f.VideoCodecs {
		hardwareDeviceType := explicitCodecCandidateHardwareDeviceType(codecName)
		hardwareDeviceName := HardwareDeviceName("")
		if hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
			hardwareDeviceName = f.HardwareDeviceName
		}
		candidate, err := newCodecCandidate(ctx, codecName, hardwareDeviceType, hardwareDeviceName, f.VideoOptions, true)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (f *NaiveEncoderFactory) Reset(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &f.Locker, f.reset, ctx)
}

// SetVideoOption sets a video-encoder open-time option key=value on the
// factory's live VideoOptions Dictionary. It allocates the Dictionary if
// nil and acquires f.Locker; callers must NOT hold f.Locker. The option is
// applied to encoders constructed from this point on; encoders that have
// already been opened are unaffected (they captured a clone of the
// Dictionary at NewEncoder time — see codec_params.go's deep-copy of
// CustomOptions). To re-open an already-opened encoder against the new
// option, call ResetHard on the bound kernel.Encoder so the next frame
// triggers a fresh NewEncoder.
//
// SetVideoOption is the encapsulated entry point for cross-package
// callers (e.g. preset/streammux's late pix_fmt injection); reaching
// directly into f.VideoOptions skips the Locker and breaks the boundary
// the factory owns.
func (f *NaiveEncoderFactory) SetVideoOption(
	ctx context.Context,
	key, value string,
) error {
	return xsync.DoR1(ctx, &f.Locker, func() error {
		if f.VideoOptions == nil {
			f.VideoOptions = astiav.NewDictionary()
		}
		if err := f.VideoOptions.Set(key, value, 0); err != nil {
			return fmt.Errorf("unable to set video encoder option %q=%q: %w", key, value, err)
		}
		return nil
	})
}

// GetVideoOption reads the current value of a video-encoder open-time
// option from the factory's live VideoOptions Dictionary, returning nil
// if the key is unset. Acquires f.Locker; callers must NOT hold it.
func (f *NaiveEncoderFactory) GetVideoOption(
	ctx context.Context,
	key string,
) *string {
	return xsync.DoR1(ctx, &f.Locker, func() *string {
		if f.VideoOptions == nil {
			return nil
		}
		entry := f.VideoOptions.Get(key, nil, 0)
		if entry == nil {
			return nil
		}
		v := entry.Value()
		return &v
	})
}

// SetVideoOptionIfAbsent atomically sets key=value only when key is not
// already present in VideoOptions. Returns whether a write happened.
// This is the atomic variant of "GetVideoOption then SetVideoOption" —
// concurrent callers cannot race past the absence check. Allocates the
// Dictionary if nil; acquires f.Locker.
func (f *NaiveEncoderFactory) SetVideoOptionIfAbsent(
	ctx context.Context,
	key, value string,
) (wrote bool, _ error) {
	return xsync.DoR2(ctx, &f.Locker, func() (bool, error) {
		if f.VideoOptions == nil {
			f.VideoOptions = astiav.NewDictionary()
		}
		if entry := f.VideoOptions.Get(key, nil, 0); entry != nil {
			return false, nil
		}
		if err := f.VideoOptions.Set(key, value, 0); err != nil {
			return false, fmt.Errorf("unable to set video encoder option %q=%q: %w", key, value, err)
		}
		return true, nil
	})
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
		videoResolution := *f.VideoResolution
		targetW, targetH := int(f.VideoResolution.Width), int(f.VideoResolution.Height)
		frameW, frameH := codecParams.Width(), codecParams.Height()

		// When autorotate applies 90°/270° rotation, the decoded frame dimensions
		// are the transpose of the configured resolution. Use the post-rotation
		// dimensions so the encoder matches what the decoder actually produces.
		if frameW == targetH && frameH == targetW && targetW != targetH {
			logger.Debugf(ctx, "frame dimensions %dx%d are rotated from configured %dx%d; using post-rotation dimensions",
				frameW, frameH, targetW, targetH)
			videoResolution.Width = uint32(frameW)
			videoResolution.Height = uint32(frameH)
		}

		logger.Tracef(ctx, "applying video resolution %#+v", videoResolution)
		codecParams.SetWidth(int(videoResolution.Width))
		codecParams.SetHeight(int(videoResolution.Height))
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
