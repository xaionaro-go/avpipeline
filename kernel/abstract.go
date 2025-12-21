package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Abstract = types.Abstract

type Flusher interface {
	IsDirty(ctx context.Context) bool
	Flush(
		ctx context.Context,
		outputPacketCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
}

type HookFunc func(ctx context.Context, input Abstract) error

func (f HookFunc) FireHook(ctx context.Context, input Abstract) error {
	return f(ctx, input)
}
