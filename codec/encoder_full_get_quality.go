// encoder_full_get_quality.go provides quality retrieval methods for the full encoder.

package codec

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().GetQuality, ctx)
}

func (e *EncoderFullLocked) GetQuality(
	ctx context.Context,
) Quality {
	if e.codecContext == nil {
		panic(fmt.Errorf("e.codecContext == nil"))
	}
	bitRate := e.codecContext.BitRate()
	if bitRate != 0 {
		return quality.ConstantBitrate(e.codecContext.BitRate())
	}
	return nil
}
