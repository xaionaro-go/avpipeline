// decoder_factory.go defines the DecoderFactory interface and its naive implementation.

package codec

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type DecoderFactory interface {
	fmt.Stringer

	NewDecoder(
		ctx context.Context,
		source packet.Source,
		stream *astiav.Stream,
		pipelineSideData globaltypes.PipelineSideData,
		opts ...Option,
	) (*Decoder, error)

	Reset(ctx context.Context) error
}

type NaiveDecoderFactory struct {
	NaiveDecoderFactoryParams
	Locker        xsync.Mutex
	VideoDecoders []*Decoder
	AudioDecoders []*Decoder
}

var _ DecoderFactory = (*NaiveDecoderFactory)(nil)

type NaiveDecoderFactoryParams struct {
	VideoCodec            Name
	VideoCodecs           []Name
	AudioCodec            Name
	AudioCodecs           []Name
	HardwareDeviceType    HardwareDeviceType
	HardwareDeviceName    HardwareDeviceName
	VideoOptions          *astiav.Dictionary
	AudioOptions          *astiav.Dictionary
	ErrorRecognitionFlags astiav.ErrorRecognitionFlags
	PreInitFunc           func(context.Context, *astiav.Stream, *DecoderInput)
	PostInitFunc          func(context.Context, *Decoder)
	ResourceManager       ResourceManager
	Options               []Option

	// AutoSelectHardwareDecoder, when true and VideoCodec is empty,
	// resolves the video decoder by trying <codec_id>_<hw_suffix>
	// against astiav.FindDecoderByName (e.g. av1_cuvid for CUDA,
	// av1_mediacodec for MediaCodec, h264_qsv for QSV). The hw_suffix
	// is derived generically from HardwareDeviceType via Name.hwName,
	// so any registered hwaccel variant is discoverable without a
	// per-codec allowlist. HardwareDeviceTypeNone defaults to CUDA
	// for backward-compat (avd cascade transcoder leaves
	// hardware_device_type unset). Falls back to the libav default if
	// the hardware decoder is not registered. An explicit VideoCodec
	// always wins.
	AutoSelectHardwareDecoder bool
}

func DefaultNaiveDecoderFactory() *NaiveDecoderFactoryParams {
	return &NaiveDecoderFactoryParams{}
}

func NewNaiveDecoderFactory(
	ctx context.Context,
	params *NaiveDecoderFactoryParams,
) *NaiveDecoderFactory {
	if params == nil {
		params = DefaultNaiveDecoderFactory()
	}
	return &NaiveDecoderFactory{
		NaiveDecoderFactoryParams: *params,
	}
}

func (f *NaiveDecoderFactory) NewDecoder(
	ctx context.Context,
	_ packet.Source,
	stream *astiav.Stream,
	_ globaltypes.PipelineSideData,
	opts ...Option,
) (_ret *Decoder, _err error) {
	return xsync.DoA3R2(ctx, &f.Locker, f.newDecoder, ctx, stream, opts)
}

func (f *NaiveDecoderFactory) newDecoder(
	ctx context.Context,
	stream *astiav.Stream,
	opts []Option,
) (_ret *Decoder, _err error) {
	if fn := f.PostInitFunc; fn != nil {
		defer func() {
			if _err != nil {
				return
			}
			f.PostInitFunc(ctx, _ret)
		}()
	}

	codecParameters := stream.CodecParameters()
	if codecParameters == nil {
		return nil, fmt.Errorf("stream %d has nil codec parameters", stream.Index())
	}

	defer func() {
		if _err != nil {
			return
		}
		switch codecParameters.MediaType() {
		case astiav.MediaTypeAudio:
			f.AudioDecoders = append(f.AudioDecoders, _ret)
		case astiav.MediaTypeVideo:
			f.VideoDecoders = append(f.VideoDecoders, _ret)
		}
	}()

	optsCombined := make([]Option, 0, len(f.Options)+len(opts))
	optsCombined = append(optsCombined, f.Options...)
	optsCombined = append(optsCombined, opts...)

	var candidates []codecCandidate
	var err error
	switch codecParameters.MediaType() {
	case astiav.MediaTypeAudio:
		candidates, err = f.audioDecoderCandidates(ctx)
		if err != nil {
			return nil, err
		}
	case astiav.MediaTypeVideo:
		candidates, err = f.videoDecoderCandidates(ctx, codecParameters.CodecID())
		if err != nil {
			return nil, err
		}
	default:
		// Return nil for unsupported media types (e.g. subtitles, data streams
		// like timed_id3 in HLS/MPEG-TS). The caller should skip packets for
		// streams that have no decoder.
		return nil, nil

	}

	var errs []error
	for _, candidate := range candidates {
		if err := validateCodecCandidate(ctx, false, candidate, codecParameters.CodecID()); err != nil {
			errs = append(errs, err)
			continue
		}

		decInput := DecoderInput{
			CodecName:             candidate.CodecName,
			CodecParameters:       codecParameters,
			HardwareDeviceType:    candidate.HardwareDeviceType,
			HardwareDeviceName:    candidate.HardwareDeviceName,
			ErrorRecognitionFlags: f.ErrorRecognitionFlags,
			CustomOptions:         candidate.CustomOptions,
			Flags:                 0,
			ResourceManager:       f.ResourceManager,
			Options:               optsCombined,
		}
		if fn := f.PreInitFunc; fn != nil {
			fn(ctx, stream, &decInput)
		}
		dec, err := NewDecoder(
			ctx,
			decInput,
		)
		if err == nil {
			return dec, nil
		}
		if !isRetryableCodecCandidateError(err) {
			return nil, err
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func (f *NaiveDecoderFactory) audioDecoderCandidates(
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

func (f *NaiveDecoderFactory) videoDecoderCandidates(
	ctx context.Context,
	codecID astiav.CodecID,
) ([]codecCandidate, error) {
	if len(f.VideoCodecs) > 0 {
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
	if f.VideoCodec != "" {
		candidate, err := newCodecCandidate(ctx, f.VideoCodec, f.HardwareDeviceType, f.HardwareDeviceName, f.VideoOptions, false)
		if err != nil {
			return nil, err
		}
		return []codecCandidate{candidate}, nil
	}
	if f.AutoSelectHardwareDecoder {
		var result []codecCandidate
		if codecName, hardwareDeviceType := preferredHWDecoderNameAndHardwareDeviceType(ctx, codecID, f.HardwareDeviceType); codecName != "" {
			candidate, err := newCodecCandidate(ctx, codecName, hardwareDeviceType, f.HardwareDeviceName, f.VideoOptions, true)
			if err != nil {
				return nil, err
			}
			result = append(result, candidate)
		}
		candidate, err := newCodecCandidate(ctx, "", globaltypes.HardwareDeviceTypeNone, "", f.VideoOptions, false)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
		return result, nil
	}
	candidate, err := newCodecCandidate(ctx, "", f.HardwareDeviceType, f.HardwareDeviceName, f.VideoOptions, false)
	if err != nil {
		return nil, err
	}
	return []codecCandidate{candidate}, nil
}

func (f *NaiveDecoderFactory) Reset(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &f.Locker, f.reset, ctx)
}

func (f *NaiveDecoderFactory) reset(
	ctx context.Context,
) error {
	var errs []error
	f.AudioDecoders = nil
	f.VideoDecoders = nil
	return errors.Join(errs...)
}

func (f *NaiveDecoderFactory) String() string {
	if len(f.VideoCodecs) == 0 && len(f.AudioCodecs) == 0 {
		return fmt.Sprintf("NaiveDecoderFactory(%s/%s)", f.VideoCodec, f.AudioCodec)
	}
	return fmt.Sprintf("NaiveDecoderFactory(%v:%s/%v:%s)", f.VideoCodecs, f.VideoCodec, f.AudioCodecs, f.AudioCodec)
}

var _ ResourcesGetter = (*NaiveDecoderFactory)(nil)

func (f *NaiveDecoderFactory) GetResources(
	ctx context.Context,
	isEncoder bool,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...Option,
) (_ret *Resources) {
	logger.Tracef(ctx, "GetResources: %#+v, %s, %#+v", params, timeBase, opts)
	defer func() { logger.Tracef(ctx, "/GetResources: %#+v, %s, %#+v: %#+v", params, timeBase, opts, _ret) }()
	return xsync.DoA4R1(ctx, &f.Locker, f.getResources, ctx, params, timeBase, opts)
}

func (f *NaiveDecoderFactory) getResources(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts []Option,
) (_ret *Resources) {
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionGetDecoderer](opts); ok {
		getDecoderer := v.GetDecoderer
		if getDecoderer != nil {
			d := getDecoderer.GetDecoder()
			logger.Debugf(ctx, "got the decoder from frame source: %v", d)
			for _, decoder := range f.VideoDecoders {
				if decoder == d {
					return resourcesFromDecoder(ctx, decoder)
				}
			}
			return nil
		}
	}

	logger.Warnf(ctx, "guessing the decoder; this is not reliable; consider using EncoderFactoryOptionFrameSource")
	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		if len(f.VideoDecoders) == 0 {
			logger.Warnf(ctx, "no video decoders are present")
			return nil
		}
		if len(f.VideoDecoders) > 1 {
			logger.Warnf(ctx, "multiple video decoders are present")
			return nil
		}
		return resourcesFromDecoder(ctx, f.VideoDecoders[0])
	case astiav.MediaTypeAudio:
		return nil
	}
	return nil
}

// ResourcesFromDecoder is the exported variant of resourcesFromDecoder for
// callers (e.g. streammux's outputAsResourceManager.GetReusable) that need
// to harvest hardware-side state from a decoder owned by a *different*
// factory than the local f.VideoDecoders set. The same lifetime caveats
// apply as resourcesFromDecoder — see its docstring.
func ResourcesFromDecoder(
	ctx context.Context,
	d *Decoder,
) *Resources {
	return resourcesFromDecoder(ctx, d)
}

// resourcesFromDecoder snapshots the decoder's hardware-side state for an
// encoder to optionally reuse. The hw_frames_ctx is populated lazily by the
// driver (cuvid attaches it after the first decoded frame), so HWFramesContext
// may be nil here even when hardware decoding is configured. The encoder side
// re-validates dims/formats before reusing.
//
// Dims (HWFramesContextWidth/Height) are recorded unconditionally from the
// decoder's CodecContext whenever a HWDeviceContext is present. The
// "hw_frames_ctx is real" semantic is checked explicitly via
// HWFramesContext != nil at every reuse site. Mediacodec decoders in
// Surface mode never attach a hw_frames_ctx (FFmpeg's mediacodec hwctx
// omits frames_init/transfer_data) yet still expose a HWDeviceContext
// whose embedded native_window IS the decoder→encoder Surface link.
// Recording the decoder's dims lets selectMediaCodecEncoderDefaultPixFmt
// keep the encoder on pix_fmt=MEDIACODEC for same-dim mediacodec→mediacodec
// passthrough — the only path that produces packets without av_hwframe_*
// scaffolding that mediacodec hwctx cannot satisfy.
//
// Lifecycle safety for HWFramesContext (an *AVBufferRef wrapper): astiav has
// no public Ref()/Clone() on HardwareFramesContext, so we cannot bump the
// refcount here. Safety relies on call ordering enforced by the caller chain:
//  1. resourcesFromDecoder is invoked under f.Locker while the decoder is
//     still registered in f.VideoDecoders (i.e. has not been Closed/Free'd).
//  2. The returned *Resources flows immediately into encoder construction
//     (NewCodec → initHardwareFramesContext) which calls
//     CodecContext.SetHardwareFramesContext(hfc) — that invokes
//     C.av_buffer_ref under the hood (astiav codec_context.go:436-445), so
//     the encoder owns its own AVBufferRef from that moment on.
//  3. After the encoder is open, the decoder's lifetime is irrelevant: the
//     underlying AVHWFramesContext stays alive as long as any ref exists.
//
// The bare hfc pointer must NOT be cached past the encoder-open call.
func resourcesFromDecoder(
	ctx context.Context,
	d *Decoder,
) *Resources {
	res := &Resources{
		HWDeviceContext:    d.HardwareDeviceContext(ctx),
		HardwareDeviceType: d.InitParams.HardwareDeviceType,
	}
	cc := d.CodecContext(ctx)
	if cc != nil && res.HWDeviceContext != nil {
		// Record decoder dims unconditionally; cuvid path (initHardwarePixelFormat
		// + initHardwareFramesContext) still gates hfc-reuse on HWFramesContext != nil
		// AND dim-match, so populating dims without a real hfc cannot mis-route
		// the cuvid encoder.
		res.HWFramesContextWidth = cc.Width()
		res.HWFramesContextHeight = cc.Height()
	}
	if hfc := d.HardwareFramesContext(ctx); hfc != nil {
		// The decoder's CodecContext.PixelFormat() returns the HW pixfmt
		// (e.g. cuda) selected via the get_format callback — that matches the
		// encoder's hardwarePixelFormat for nvenc, so it's the right field to
		// compare against. The SW pixfmt is opaque (no getter on astiav's
		// HardwareFramesContext), so the encoder side does not validate it.
		if cc != nil {
			res.HWFramesContext = hfc
			res.HWFramesContextHWPixFmt = d.HardwarePixelFormat(ctx)
			logger.Debugf(ctx, "captured upstream hw_frames_ctx: %p %dx%d hw=%s",
				hfc, res.HWFramesContextWidth, res.HWFramesContextHeight,
				res.HWFramesContextHWPixFmt,
			)
		}
	}
	return res
}
