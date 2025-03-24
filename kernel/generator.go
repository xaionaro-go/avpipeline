package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Generator interface {
	Generate(
		ctx context.Context,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
}
