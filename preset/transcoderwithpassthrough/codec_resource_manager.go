// codec_resource_manager.go adapts TranscoderWithPassthrough as a codec
// ResourceManager so the encoder can borrow the decoder's hw_frames_ctx
// instead of allocating a fresh one (avoids HW->SW->HW round-trip).

package transcoderwithpassthrough

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/processor"
)

const (
	// transcoderReuseDecoderResources gates the decoder->encoder hw_frames_ctx
	// reuse path for cuvid->nvenc (avoids the HW->SW->HW round-trip).
	//
	// Defaults to true here. Streammux's analog (outputReuseDecoderResources)
	// defaults to false because streammux's broader unused-output borrowing
	// feature is WIP; that complexity does not apply to TranscoderWithPassthrough,
	// which owns a single transcoder with one decoder feeding one encoder.
	transcoderReuseDecoderResources = true
)

type transcoderAsResourceManager[C any, P processor.Abstract] TranscoderWithPassthrough[C, P]

var _ resource.ResourceManager = (*transcoderAsResourceManager[any, processor.Abstract])(nil)

func (s *TranscoderWithPassthrough[C, P]) asCodecResourceManager() *transcoderAsResourceManager[C, P] {
	return (*transcoderAsResourceManager[C, P])(s)
}

func (rm *transcoderAsResourceManager[C, P]) asTranscoder() *TranscoderWithPassthrough[C, P] {
	return (*TranscoderWithPassthrough[C, P])(rm)
}

func (rm *transcoderAsResourceManager[C, P]) GetReusable(
	ctx context.Context,
	isEncoder bool,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codectypes.Option,
) (_ret *resource.Resources) {
	logger.Debugf(ctx, "GetReusable")
	defer func() { logger.Debugf(ctx, "/GetReusable: %v", _ret) }()
	if !transcoderReuseDecoderResources {
		logger.Tracef(ctx, "GetReusable: feature gate off")
		return nil
	}
	if !isEncoder {
		logger.Tracef(ctx, "GetReusable: not an encoder")
		return nil
	}
	if params.MediaType() != astiav.MediaTypeVideo {
		logger.Tracef(ctx, "GetReusable: media type is not video (%s)", params.MediaType())
		return nil
	}
	if !rm.canReuse(ctx, params, opts...) {
		logger.Tracef(ctx, "GetReusable: canReuse returned false")
		return nil
	}
	return rm.asTranscoder().Transcoder.DecoderFactory.GetResources(
		ctx, false /* querying decoder-side resources */, params, timeBase, opts...,
	)
}

func (rm *transcoderAsResourceManager[C, P]) canReuse(
	ctx context.Context,
	params *astiav.CodecParameters,
	opts ...codectypes.Option,
) (_ret bool) {
	logger.Debugf(ctx, "canReuse")
	defer func() { logger.Debugf(ctx, "/canReuse: %v", _ret) }()
	encFactory := rm.asTranscoder().Transcoder.EncoderFactory
	encRes := encFactory.VideoResolution
	if encRes == nil {
		return false
	}
	getDecoderer, ok := codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](opts)
	if !ok {
		return false
	}
	if params.Width() != int(encRes.Width) {
		return false
	}
	if params.Height() != int(encRes.Height) {
		return false
	}
	decoder := getDecoderer.GetDecoderer.GetDecoder()
	if decoder == nil {
		return false
	}
	if params.PixelFormat() != astiav.PixelFormatNone {
		cc := decoder.CodecContext(ctx)
		if cc == nil {
			return false
		}
		if params.PixelFormat() != cc.PixelFormat() {
			return false
		}
	}
	return true
}

func (rm *transcoderAsResourceManager[C, P]) FreeUnneeded(
	ctx context.Context,
	resourceType resource.Type,
	c *astiav.Codec,
	opts ...codectypes.Option,
) uint {
	// TWP holds a single transcoder; nothing to free on behalf of siblings.
	return 0
}
