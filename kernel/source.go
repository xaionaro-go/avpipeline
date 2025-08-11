package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
)

func sourceNbStreams(ctx context.Context, s packet.Source) (_ret int) {
	logger.Tracef(ctx, "sourceNbStreams: %s", s)
	defer func() { logger.Tracef(ctx, "/sourceNbStreams: %s: %v", s, _ret) }()
	var result int
	s.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		result = fmtCtx.NbStreams()
	})
	return result
}
