package codec

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) GetResolution(
	ctx context.Context,
) (uint32, uint32) {
	return xsync.DoA1R2(xsync.WithNoLogging(ctx, true), &e.locker, e.getResolutionLocked, ctx)
}

func (e *EncoderFull) getResolutionLocked(
	ctx context.Context,
) (uint32, uint32) {
	if e.codecContext == nil {
		panic(fmt.Errorf("e.codecContext == nil"))
	}
	return uint32(e.codecContext.Width()), uint32(e.codecContext.Height())
}
