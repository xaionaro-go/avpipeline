//go:build !patched_libav
// +build !patched_libav

package codec

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avmediacodec"
)

func mediaCodecSetParameters(
	ctx context.Context,
	mediaCodec *avmediacodec.FFAMediaCodec,
	mediaCodecFmt *avmediacodec.FFAMediaFormat,
) error {
	logger.Debugf(ctx, "using the NDK's SetParameters to set the parameters")
	return mediaCodec.SetParametersNDK(mediaCodecFmt)
}
