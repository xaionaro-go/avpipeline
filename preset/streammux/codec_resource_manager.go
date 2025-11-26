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

	return rm.asOutput().RecoderNode.Processor.Kernel.DecoderFactory.GetResources(
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

	encRes := rm.asOutput().RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution

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

	// we can reuse the resources only if pixel format is the same
	decoder := getDecoderer.GetDecoderer.GetDecoder()
	if params.PixelFormat() != astiav.PixelFormatNone {
		if params.PixelFormat() != decoder.CodecContext().PixelFormat() {
			logger.Tracef(ctx, "pixel format mismatch: params=%v vs decoder=%v", params.PixelFormat(), decoder.CodecContext().PixelFormat())
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
		if key == activeVideoOutput.ID || key == activeAudioOutput.ID {
			return true
		}
		err := output.ResetRecoder(ctx)
		switch {
		case err == nil:
		case errors.As(err, &ErrNothingToReset{}):
			logger.Tracef(ctx, "nothing to reset for output %v", key)
			return true
		default:
			logger.Errorf(ctx, "unable to reset recoder of output %v: %v", key, err)
			return true
		}
		_ret++
		return true
	})
	return
}

func (o *Output[C]) ResetRecoder(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetRecoder")
	defer func() { logger.Debugf(ctx, "/ResetRecoder: %v", _err) }()
	if len(o.RecoderNode.Processor.Kernel.DecoderFactory.VideoDecoders) == 0 &&
		len(o.RecoderNode.Processor.Kernel.DecoderFactory.AudioDecoders) == 0 &&
		len(o.RecoderNode.Processor.Kernel.EncoderFactory.VideoEncoders) == 0 &&
		len(o.RecoderNode.Processor.Kernel.EncoderFactory.AudioEncoders) == 0 {
		return ErrNothingToReset{}
	}
	err := o.RecoderNode.Processor.Kernel.ResetHard(ctx)
	if err != nil {
		return fmt.Errorf("unable to reset recoder kernel: %w", err)
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
