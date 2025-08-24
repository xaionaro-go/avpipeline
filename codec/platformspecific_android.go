//go:build android
// +build android

package codec

import (
	"context"
	"encoding/json"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/androidetc"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

var (
	mediaCodecsInfoLocker xsync.Mutex
	mediaCodecsInfo       androidetc.MediaCodecsDescriptors
)

func (c *Codec) platformSpecificHWSanityChecks(ctx context.Context) {
	logger.Tracef(ctx, "platformSpecificHWSanityChecks")
	defer func() { logger.Tracef(ctx, "/platformSpecificHWSanityChecks") }()

	if c.MediaType() != astiav.MediaTypeVideo {
		return
	}

	mimeType := c.GetAndroidMIMEType()
	if len(mimeType) == 0 {
		logger.Tracef(ctx, "no mime types found")
		return
	}

	mediaCodecsInfoLocker.Do(ctx, func() {
		if mediaCodecsInfo == nil {
			var err error
			mediaCodecsInfo, err = androidetc.ParseMediaCodecs()
			if err != nil {
				logger.Warnf(ctx, "failed to parse media codecs info: %v", err)
				return
			}
		}

		for _, codecInfo := range mediaCodecsInfo {
			var codecs []androidetc.MediaCodec
			if c.IsEncoder {
				codecs = codecInfo.Encoders
			} else {
				codecs = codecInfo.Decoders
			}
			for _, codec := range codecs {
				isFitting := false
				for _, typ := range codec.Types {
					if typ.Name == mimeType {
						isFitting = true
						break
					}
				}
				if !isFitting {
					isFitting = codec.Type == mimeType
				}
				if !isFitting {
					continue
				}
				if !codec.IsHardware() {
					continue
				}
				b, _ := json.Marshal(codec)
				logger.Tracef(ctx, "found fitting hardware codec: %s", string(b))
				return
			}
		}

		logger.Warnf(ctx, "no fitting hardware codec found for mime types %q (isEncoder=%v)", mimeType, c.IsEncoder)
	})
}
