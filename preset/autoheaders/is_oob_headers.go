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
	default:
		logger.Debugf(ctx, "the format '%s' is unknown, so defaulting to in-band headers", formatName)
		fallthrough
	case "mpegts", "rtsp":
		return false
	case "flv":
		return true
	}
}
