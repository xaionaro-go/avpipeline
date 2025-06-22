package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/libsrt/extras/xastiav"
)

func formatContextToSRTFD(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
) (int, error) {
	return int(xastiav.GetFDFromFormatContext(fmtCtx)), nil
}
