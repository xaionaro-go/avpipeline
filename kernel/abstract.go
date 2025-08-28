package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Abstract = types.Abstract
type IsDirtier = types.IsDirtier

type Flusher interface {
	Flush(
		ctx context.Context,
		outputPacketCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
}
