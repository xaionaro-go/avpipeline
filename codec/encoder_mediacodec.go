//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avmediacodec"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/xsync"
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

func (e *EncoderFull) FFAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	logger.Debugf(ctx, "FFAMediaFormatSetInt32(ctx, '%s', %d)", key, value)
	defer func() { logger.Debugf(ctx, "/FFAMediaFormatSetInt32(ctx, '%s', %d): %v", key, value, _err) }()
	return xsync.DoA3R1(ctx, &e.locker, e.ffAMediaFormatSetInt32, ctx, key, value)
}

func (e *EncoderFull) ffAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	mediaCodec := avmediacodec.WrapAVCodecContext(
		xastiav.CFromAVCodecContext(e.codecContext),
	).PrivData().Codec()

	mediaCodecFmt := mediaCodec.Format()
	mediaCodecFmt.SetInt32(key, value)
	result, err := mediaCodecFmt.GetInt32(key)
	if err != nil {
		return fmt.Errorf("unable to get the current value of '%s': %w", key, err)
	}
	logger.Tracef(ctx, "resulting value: %d", result)
	if result != value {
		return fmt.Errorf("verification failed: requested value is %d, but the resulting value is %d", value, result)
	}
	err = mediaCodecSetParameters(ctx, mediaCodec, mediaCodecFmt)
	if err != nil {
		return fmt.Errorf("unable to SetParameters: %w", err)
	}

	e.codecContext.SetBitRate(int64(value))

	return nil
}
