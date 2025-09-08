//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/mediacodec"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/quality"
)

func (e *EncoderFullLocked) setQualityMediacodec(
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

func (e *EncoderFullLocked) setQualityMediacodecConstantBitrate(
	ctx context.Context,
	q quality.ConstantBitrate,
) error {
	if err := e.ffAMediaFormatSetInt32(ctx, mediacodec.PARAMETER_KEY_VIDEO_BITRATE, int32(q)); err != nil {
		return fmt.Errorf("unable to set video-bitrate: %w", err)
	}

	e.codecContext.SetBitRate(int64(q))
	return nil
}
