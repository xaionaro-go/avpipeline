//go:build mediacodec
// +build mediacodec

package codec

import (
	"context"
	"fmt"

	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avmediacodec"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

func (c *Codec) FFAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	logger.Debugf(ctx, "FFAMediaFormatSetInt32(ctx, '%s', %d)", key, value)
	defer func() { logger.Debugf(ctx, "/FFAMediaFormatSetInt32(ctx, '%s', %d): %v", key, value, _err) }()
	return xsync.DoA3R1(ctx, &c.locker, c.ffAMediaFormatSetInt32, ctx, key, value)
}

func (c *Codec) ffAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	mediaCodec := avmediacodec.WrapAVCodecContext(
		xastiav.CFromAVCodecContext(c.codecContext),
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

	return nil
}
