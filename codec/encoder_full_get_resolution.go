// encoder_full_get_resolution.go provides resolution retrieval methods for the full encoder.

package codec

import (
	"context"

	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) GetResolution(
	ctx context.Context,
) *Resolution {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().GetResolution, ctx)
}

func (e *EncoderFullLocked) GetResolution(
	ctx context.Context,
) *Resolution {
	if e.codecContext == nil {
		return nil
	}
	return &Resolution{
		Width:  uint32(e.codecContext.Width()),
		Height: uint32(e.codecContext.Height()),
	}
}
