package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type FrameCondition struct {
	condition.Condition
}

var _ Abstract = (*FrameCondition)(nil)

func NewFrameCondition(
	cond condition.Condition,
) *FrameCondition {
	return &FrameCondition{
		Condition: cond,
	}
}

func (*FrameCondition) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	return fmt.Errorf("FrameCondition does not process packets")
}

func (k *FrameCondition) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()

	if !k.Condition.Match(ctx, input) {
		logger.Tracef(ctx, "frame does not match condition, dropping: stream:%d frame:%p pts:%d", input.StreamIndex, input.Frame, input.Frame.Pts())
		return nil
	}

	return k.passthrough(ctx, input, outputFramesCh)
}

func (k *FrameCondition) passthrough(
	ctx context.Context,
	input frame.Input,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "passthrough")
	defer func() { logger.Tracef(ctx, "/passthrough: %v", _err) }()
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

func (k *FrameCondition) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (*FrameCondition) String() string {
	return "AudioNormalize"
}

func (*FrameCondition) Close(context.Context) error {
	return nil
}

func (*FrameCondition) CloseChan() <-chan struct{} {
	return nil
}

func (*FrameCondition) Generate(
	ctx context.Context,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
	return nil
}
