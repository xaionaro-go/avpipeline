package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
)

type Passthrough struct{}

var _ Abstract = (*Passthrough)(nil)

func (Passthrough) SendInput(
	ctx context.Context,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
) error {
	outputCh <- types.BuildOutputPacket(
		packet.CloneAsReferenced(input.Packet),
		input.FormatContext,
	)
	return nil
}

func (Passthrough) String() string {
	return "Noop"
}

func (Passthrough) Close(context.Context) error {
	return nil
}

func (Passthrough) CloseChan() <-chan struct{} {
	return nil
}

func (Passthrough) Generate(
	ctx context.Context,
	outputCh chan<- types.OutputPacket,
) error {
	return nil
}
