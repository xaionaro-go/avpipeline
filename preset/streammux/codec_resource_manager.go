// codec_resource_manager.go implements a resource manager for reusing codec resources.

package streammux

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type outputAsResourceManager[C any] Output[C]

var _ resource.ResourceManager = (*outputAsResourceManager[any])(nil)

func (o *Output[C]) asCodecResourceManager() *outputAsResourceManager[C] {
	return (*outputAsResourceManager[C])(o)
}

func (rm *outputAsResourceManager[C]) asOutput() *Output[C] {
	return (*Output[C])(rm)
}

func (rm *outputAsResourceManager[C]) GetReusable(
	ctx context.Context,
	isEncoder bool,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codectypes.Option,
) (_ret *resource.Resources) {
	logger.Debugf(ctx, "GetReusable")
	defer func() { logger.Debugf(ctx, "/GetReusable: %v", _ret) }()

	if !outputReuseDecoderResources {
		logger.Tracef(ctx, "outputReuseDecoderResources is disabled")
		return nil
	}

	if !isEncoder {
		logger.Tracef(ctx, "not an encoder, so no reusable resources")
		return nil
	}

	if !rm.canReuse(ctx, isEncoder, params, timeBase, opts...) {
		logger.Tracef(ctx, "cannot reuse the resources")
		return nil
	}

	if params.MediaType() != astiav.MediaTypeVideo {
		logger.Tracef(ctx, "only 'video' media type is supported for reusing the resources")
		return nil
	}

	decFactory := rm.asOutput().TranscoderNode.Processor.Kernel.DecoderFactory
	// When the streammux's internal Transcoder decoder is bypassed (the
	// upstream pipeline already produced decoded frames and pushed them via
	// SendInput in kernel/transcoder.go), decFactory.VideoDecoders stays
	// empty. In that case GetResources cannot match the upstream decoder
	// against its own registry and returns nil, which prevents surface
	// passthrough. Fall back to harvesting Resources directly from the
	// upstream decoder advertised through EncoderFactoryOptionGetDecoderer.
	if len(decFactory.VideoDecoders) == 0 {
		if v, ok := codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](opts); ok &&
			v.GetDecoderer != nil {
			d := v.GetDecoderer.GetDecoder()
			if d != nil {
				return codec.ResourcesFromDecoder(ctx, d)
			}
		}
	}
	return decFactory.GetResources(
		ctx,
		isEncoder,
		params,
		timeBase,
		opts...,
	)
}

func (rm *outputAsResourceManager[C]) canReuse(
	ctx context.Context,
	isEncoder bool,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codectypes.Option,
) (_ret bool) {
	// allow sharing the resources between decoder and encoder
	logger.Debugf(ctx, "canReuse")
	defer func() { logger.Debugf(ctx, "/canReuse: %v", _ret) }()

	// nil-guard params: cascade init can fire reuse queries before the
	// upstream stream parameters are bound — same timing class as the
	// existing decoder/CodecContext nil cases handled below. Without codec
	// parameters we cannot determine width/height/pixel-format compatibility,
	// so conservatively decline reuse and let the caller fall through to
	// creating a fresh resource pool.
	if params == nil {
		logger.Debugf(ctx, "params is nil; cannot determine compatibility, skipping reuse")
		return false
	}

	encRes := rm.asOutput().TranscoderNode.Processor.Kernel.EncoderFactory.VideoResolution
	// nil-guard encRes: encoder factory cascade init can fire reuse
	// queries before VideoResolution is bound. The field is *Resolution
	// (codec/encoder_factory.go: NaiveEncoderFactoryParams.VideoResolution),
	// and Resolution.Width is at offset 0
	// (codec/types/resolution.go: type Resolution struct { Width uint32; Height uint32 }),
	// so a nil VideoResolution dereference at the width comparison below
	// faults at addr=0x0. Same conservative-decline policy as the params
	// nil-guard above: we cannot determine compatibility without the
	// target resolution, so let the caller fall through to creating a
	// fresh resource pool.
	if encRes == nil {
		logger.Debugf(ctx, "encoder factory VideoResolution is nil; cannot determine compatibility, skipping reuse")
		return false
	}

	getDecoderer, ok := codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](opts)
	if !ok {
		logger.Debugf(ctx, "unable to find FrameSource in the EncoderFactory options: %#+v", opts)
		return false
	}

	// we can reuse the resources only if no scaling is required
	if params.Width() != int(encRes.Width) {
		logger.Tracef(ctx, "width mismatch: params=%v vs encoder=%v", params.Width(), encRes.Width)
		return false
	}
	if params.Height() != int(encRes.Height) {
		logger.Tracef(ctx, "height mismatch: params=%v vs encoder=%v", params.Height(), encRes.Height)
		return false
	}

	// we can reuse the resources only if pixel format is the same.
	//
	// nil-guard the decoder and its CodecContext: GetDecoder() can return
	// nil before the upstream decoder has been bound (raw-frame source
	// pipelines like android_camera + StreamMux's bypass branch never
	// register a decoder, and during cascade init the encoder's reuse
	// query can fire before the upstream decoder is open). CodecContext
	// can be nil for the same reason — the decoder exists but its codec
	// context hasn't been opened yet (avcodec_open2 hasn't run).
	//
	// When we can't safely read the decoder's pixel format, we
	// conservatively decline reuse — the caller falls through to creating
	// a fresh resource pool.
	decoder := getDecoderer.GetDecoderer.GetDecoder()
	if decoder == nil {
		logger.Debugf(ctx, "decoder is nil; cannot determine pixel format compatibility, skipping reuse")
		return false
	}
	if params.PixelFormat() != astiav.PixelFormatNone {
		decCC, ok := decoder.CodecContextIfAvailable(ctx)
		if !ok {
			logger.Debugf(ctx, "decoder CodecContext is busy; cannot determine pixel format compatibility, skipping reuse")
			return false
		}
		if decCC == nil {
			logger.Debugf(ctx, "decoder CodecContext is nil; cannot determine pixel format compatibility, skipping reuse")
			return false
		}
		if params.PixelFormat() != decCC.PixelFormat() {
			logger.Tracef(ctx, "pixel format mismatch: params=%v vs decoder=%v", params.PixelFormat(), decCC.PixelFormat())
			return false
		}
	}

	return true
}

func (rm *outputAsResourceManager[C]) FreeUnneeded(
	ctx context.Context,
	resourceType resource.Type,
	codec *astiav.Codec,
	opts ...codectypes.Option,
) (_ret uint) {
	logger.Debugf(ctx, "FreeUnneeded(%v)", resourceType)
	defer func() { logger.Debugf(ctx, "/FreeUnneeded(%v): %v", resourceType, _ret) }()
	return rm.ParentResourceManager.FreeUnneeded(ctx, resourceType, codec, opts...)
}

type asResourceManager[C any] StreamMux[C]

var _ ResourceManager = (*asResourceManager[any])(nil)

func (s *StreamMux[C]) asCodecResourceManager() *asResourceManager[C] {
	return (*asResourceManager[C])(s)
}

func (rm *asResourceManager[C]) asStreamMux() *StreamMux[C] {
	return (*StreamMux[C])(rm)
}

func (rm *asResourceManager[C]) FreeUnneeded(
	ctx context.Context,
	resourceType resource.Type,
	codec *astiav.Codec,
	opts ...codectypes.Option,
) (_ret uint) {
	logger.Tracef(ctx, "FreeUnneeded(%v)", resourceType)
	defer func() { logger.Tracef(ctx, "/FreeUnneeded(%v): %v", resourceType, _ret) }()
	return rm.asStreamMux().closeUnusedOutputs(ctx, resourceType, codec)
}

func (s *StreamMux[C]) closeUnusedOutputs(
	ctx context.Context,
	resourceType resource.Type,
	codec *astiav.Codec,
	opts ...codectypes.Option,
) (_ret uint) {
	logger.Tracef(ctx, "closeUnusedOutputs")
	defer func() { logger.Tracef(ctx, "/closeUnusedOutputs(%v): %v", resourceType, _ret) }()
	return xsync.DoA4R1(ctx, &s.OutputsLocker, s.closeUnusedOutputsLocked, ctx, resourceType, codec, opts)
}

func (s *StreamMux[C]) closeUnusedOutputsLocked(
	ctx context.Context,
	resourceType resource.Type,
	_ *astiav.Codec,
	opts codectypes.Options,
) (_ret uint) {
	logger.Tracef(ctx, "closeUnusedOutputsLocked")
	defer func() { logger.Tracef(ctx, "/closeUnusedOutputsLocked(%v): %v", resourceType, _ret) }()
	outIDOpt, ok := codectypes.OptionLatest[CodecOptionOutputID](opts)
	if !ok {
		logger.Errorf(ctx, "bug in the code: unable to find OutputID in the codec options: %#+v", opts)
		return
	}
	activeVideoOutput := s.getActiveVideoOutputLocked(ctx)
	activeAudioOutput := s.getActiveAudioOutputLocked(ctx)
	s.Outputs.Range(func(key OutputID, output *Output[C]) bool {
		if key == outIDOpt.OutputID {
			return true
		}
		if (activeVideoOutput != nil && key == activeVideoOutput.ID) || (activeAudioOutput != nil && key == activeAudioOutput.ID) {
			return true
		}
		err := output.ResetTranscoder(ctx)
		switch {
		case err == nil:
		case errors.As(err, &ErrNothingToReset{}):
			logger.Tracef(ctx, "nothing to reset for output %v", key)
			return true
		default:
			logger.Errorf(ctx, "unable to reset transcoder of output %v: %v", key, err)
			return true
		}
		_ret++
		return true
	})
	return
}

func (o *Output[C]) ResetTranscoder(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetTranscoder")
	defer func() { logger.Debugf(ctx, "/ResetTranscoder: %v", _err) }()
	if len(o.TranscoderNode.Processor.Kernel.DecoderFactory.VideoDecoders) == 0 &&
		len(o.TranscoderNode.Processor.Kernel.DecoderFactory.AudioDecoders) == 0 &&
		len(o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoEncoders) == 0 &&
		len(o.TranscoderNode.Processor.Kernel.EncoderFactory.AudioEncoders) == 0 {
		return ErrNothingToReset{}
	}
	err := o.TranscoderNode.Processor.Kernel.ResetHard(ctx)
	if err != nil {
		return fmt.Errorf("unable to reset transcoder kernel: %w", err)
	}
	return nil
}

type ErrNothingToReset struct{}

func (e ErrNothingToReset) Error() string {
	return "nothing to reset"
}

type CodecOptionOutputID struct {
	codec.OptionCommons
	OutputID OutputID
}

var _ codectypes.Option = CodecOptionOutputID{}
