package autoheaders

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func isOOBHeadersByFormatName(
	ctx context.Context,
	formatName string,
) bool {
	switch formatName {
	case "mpegts", "rtsp":
		return false
	default:
		logger.Debugf(ctx, "the format '%s' is unknown, so defaulting to out-of-band headers", formatName)
		fallthrough
	case "flv":
		return true
	}
}
