package processor

import (
	"context"
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
)

type IsDirtier interface {
	IsDirty(
		ctx context.Context,
	) bool
}

type ProcessingState struct {
	PendingPackets   int
	PendingFrames    int
	IsProcessorDirty bool
	IsProcessing     bool
	InputSent        atomic.Bool
}

var _ IsDirtier = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) IsDirty(ctx context.Context) bool {
	if isDirtier, ok := any(p.Kernel).(kerneltypes.IsDirtier); ok {
		return isDirtier.IsDirty(ctx)
	}
	return false
}
