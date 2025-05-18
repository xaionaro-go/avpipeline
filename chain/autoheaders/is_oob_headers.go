package autoheaders

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func isOOBHeadersByFormatName(
	ctx context.Context,
	formatName string,
) bool {
	switch formatName {
	default:
		logger.Debugf(ctx, "the output format is unknown, so defaulting to in-band headers")
		fallthrough
	case "mpegts", "rtsp":
		return false
	case "flv":
		return true
	}
}
