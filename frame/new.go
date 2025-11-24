package frame

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
)

func NewBlankVideo(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
) (_ret *astiav.Frame, _err error) {
	logger.Tracef(ctx, "NewBlankVideo(ctx, %v)", codecParams)
	defer func() { logger.Tracef(ctx, "/NewBlankVideo(ctx, %v): %v, %v", codecParams, _ret, _err) }()

	f := Pool.Get()
	defer func() {
		if _err != nil {
			Pool.Put(f)
		}
	}()

	f.SetWidth(codecParams.Width())
	f.SetHeight(codecParams.Height())
	f.SetPixelFormat(codecParams.PixelFormat())
	f.SetSampleAspectRatio(codecParams.SampleAspectRatio())
	if err := f.AllocBuffer(0); err != nil {
		return nil, fmt.Errorf("unable to allocate frame buffer: %w", err)
	}
	if err := f.ImageFillBlack(); err != nil {
		return nil, fmt.Errorf("unable to fill frame with black color: %w", err)
	}
	return f, nil
}
