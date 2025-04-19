//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avmediacodec"
	"github.com/xaionaro-go/avmediacodec/astiavmediacodec"
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
	return e.FFAMediaFormatSetInt32(ctx, "video-bitrate", int32(q))
}

func (e *EncoderFull) FFAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	logger.Tracef(ctx, "FFAMediaFormatSetInt32(ctx, '%s', %d)", key, value)
	defer func() { logger.Tracef(ctx, "/FFAMediaFormatSetInt32(ctx, '%s', %d): %v", key, value, _err) }()

	fmt := avmediacodec.WrapAVCodecContext(
		astiavmediacodec.CFromAVCodecContext(e.codecContext),
	)
	err := fmt.PrivData().Codec().Format().SetInt32(key, value)
	if err != nil {
		return err
	}

	e.codecContext.SetBitRate(int64(value))
	return nil
}
