//go:build mediacodec && !patched_libav
// +build mediacodec,!patched_libav

// mediacodec_vanilla_libav.go provides MediaCodec parameter setting logic for vanilla libav.

package codec

import (
	"context"

	"github.com/xaionaro-go/avmediacodec"
	"github.com/xaionaro-go/avpipeline/logger"
)

func mediaCodecSetParameters(
	ctx context.Context,
	mediaCodec *avmediacodec.FFAMediaCodec,
	mediaCodecFmt *avmediacodec.FFAMediaFormat,
) error {
	logger.Debugf(ctx, "using the NDK's SetParameters to set the parameters")
	return mediaCodec.SetParametersNDK(mediaCodecFmt)
}
