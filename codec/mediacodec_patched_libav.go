//go:build mediacodec && patched_libav
// +build mediacodec,patched_libav

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
	logger.Debugf(ctx, "using the libav-patched SetParameters to set the parameters")
	return mediaCodec.SetParametersPatchedLibAV(mediaCodecFmt)
}
