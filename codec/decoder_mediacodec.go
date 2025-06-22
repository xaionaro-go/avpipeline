//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func (d *Decoder) setLowLatencyMediacodec(
	ctx context.Context,
	v bool,
) error {
	logger.Infof(ctx, "SetLowLatency (MediaCodec): %v", v)
	i := int32(0)
	if v {
		i = 1
	}
	return d.ffAMediaFormatSetInt32(ctx, "low-latency", i)
}
