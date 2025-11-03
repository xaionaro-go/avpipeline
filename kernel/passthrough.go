package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Passthrough struct{}

var _ Abstract = (*Passthrough)(nil)

func (Passthrough) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputPacketsCh <- packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.StreamInfo,
	):
	}
	return nil
}

func (Passthrough) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputFramesCh <- frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.StreamInfo,
	):
	}
	return nil
}

func (Passthrough) String() string {
	return "Passthrough"
}

func (Passthrough) Close(context.Context) error {
	return nil
}

func (Passthrough) CloseChan() <-chan struct{} {
	return nil
}

func (Passthrough) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}
