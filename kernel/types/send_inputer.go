package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type SendInputer interface {
	SendInputPacketer
	SendInputFramer
}

type SendInputPacketer interface {
	SendInputPacket(
		ctx context.Context,
		input packet.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
}

type SendInputFramer interface {
	SendInputFrame(
		ctx context.Context,
		input frame.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
}
