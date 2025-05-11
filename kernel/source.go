package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
)

func sourceNbStreams(ctx context.Context, s packet.Source) int {
	var result int
	s.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		result = fmtCtx.NbStreams()
	})
	return result
}
