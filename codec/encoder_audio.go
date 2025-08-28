package codec

import (
	"context"

	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) GetPCMAudioFormat(
	ctx context.Context,
) *PCMAudioFormat {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().GetPCMAudioFormat, ctx)
}

func (e *EncoderFullLocked) GetPCMAudioFormat(
	ctx context.Context,
) *PCMAudioFormat {
	if e.codecContext == nil {
		return nil
	}
	return &PCMAudioFormat{
		SampleFormat:  e.codecContext.SampleFormat(),
		SampleRate:    e.codecContext.SampleRate(),
		ChannelLayout: e.codecContext.ChannelLayout(),
		ChunkSize:     e.codecContext.FrameSize(),
	}
}
