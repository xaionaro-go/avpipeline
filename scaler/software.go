package scaler

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
)

type Software struct {
	*astiav.SoftwareScaleContext
	*closuresignaler.ClosureSignaler
}

var _ Scaler = (*Software)(nil)

func NewSoftware(
	ctx context.Context,
	src codec.Resolution,
	srcPixFmt astiav.PixelFormat,
	dst codec.Resolution,
	dstPixFmt astiav.PixelFormat,
	opts ...astiav.SoftwareScaleContextFlag,
) (*Software, error) {
	swSCtx, err := astiav.CreateSoftwareScaleContext(
		int(src.Width),
		int(src.Height),
		srcPixFmt,
		int(dst.Width),
		int(dst.Height),
		dstPixFmt,
		astiav.NewSoftwareScaleContextFlags(opts...),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create a software scale context: %w", err)
	}
	setFinalizerFree(ctx, swSCtx)
	return &Software{
		SoftwareScaleContext: swSCtx,
		ClosureSignaler:      closuresignaler.New(),
	}, nil
}

func (s *Software) String() string {
	return fmt.Sprintf(
		"SoftwareScaler(%dx%d:%s -> %dx%d:%s)",
		s.SoftwareScaleContext.SourceWidth(),
		s.SoftwareScaleContext.SourceHeight(),
		s.SoftwareScaleContext.SourcePixelFormat(),
		s.SoftwareScaleContext.DestinationWidth(),
		s.SoftwareScaleContext.DestinationHeight(),
		s.SoftwareScaleContext.DestinationPixelFormat(),
	)
}

func (s *Software) Close(ctx context.Context) error {
	logger.Tracef(ctx, "Close")
	defer logger.Tracef(ctx, "/Close")
	s.ClosureSignaler.Close(ctx)
	return nil
}

func (s *Software) ScaleFrame(
	ctx context.Context,
	src *astiav.Frame,
	dst *astiav.Frame,
) (_err error) {
	logger.Tracef(ctx, "ScaleFrame")
	defer logger.Tracef(ctx, "/ScaleFrame: %v", _err)
	if s.IsClosed() {
		return fmt.Errorf("scaler is closed")
	}
	if err := s.SoftwareScaleContext.ScaleFrame(src, dst); err != nil {
		return fmt.Errorf("unable to scale a frame: %w", err)
	}
	return nil
}

func (s *Software) SourceResolution() codec.Resolution {
	return codec.Resolution{
		Width:  uint32(s.SoftwareScaleContext.SourceWidth()),
		Height: uint32(s.SoftwareScaleContext.SourceHeight()),
	}
}

func (s *Software) SourcePixelFormat() astiav.PixelFormat {
	return s.SoftwareScaleContext.SourcePixelFormat()
}

func (s *Software) DestinationResolution() codec.Resolution {
	return codec.Resolution{
		Width:  uint32(s.SoftwareScaleContext.DestinationWidth()),
		Height: uint32(s.SoftwareScaleContext.DestinationHeight()),
	}
}

func (s *Software) DestinationPixelFormat() astiav.PixelFormat {
	return s.SoftwareScaleContext.DestinationPixelFormat()
}
