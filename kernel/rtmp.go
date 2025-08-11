package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avpipeline/logger"
)

func formatContextToRTMPFD(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
) (_ret int, _err error) {
	logger.Tracef(ctx, "formatContextToRTMPFD")
	defer func() { logger.Tracef(ctx, "/formatContextToRTMPFD: %v %v", _ret, _err) }()

	f := avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(fmtCtx))
	avioCtx := f.Pb()
	urlCtx := avcommon.WrapURLContext(avioCtx.Opaque())
	rtmpCtx := avcommon.WrapRTMPContext(urlCtx.PrivData())
	return rtmpCtx.GetFileDescriptor(), nil
}
