//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/quality"
)

func (e *EncoderFull) setQualityMediacodec(
	ctx context.Context,
	q Quality,
) error {
	logger.Infof(ctx, "SetQuality (MediaCodec): %T(%v)", q, q)
	switch q := q.(type) {
	case quality.ConstantBitrate:
		return e.setQualityMediacodecConstantBitrate(ctx, q)
	default:
		return fmt.Errorf("unable to set quality by type %T", q)
	}
}

func (e *EncoderFull) setQualityMediacodecConstantBitrate(
	ctx context.Context,
	q quality.ConstantBitrate,
) error {
	return e.ffAMediaFormatSetInt32(ctx, "video-bitrate", int32(q))
}
