package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Passthrough struct{}

var _ Abstract = (*Passthrough)(nil)

func (Passthrough) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
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
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
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

func (p *Passthrough) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(p)
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
	ctx context.Context,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
	return nil
}
