package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/frame/condition"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
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

func (k *FrameCondition) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	pkt, frameInput := input.Unwrap()
	switch {
	case pkt != nil:
		logger.Tracef(ctx, "SendInput(packet)")
		defer func() { logger.Tracef(ctx, "/SendInput(packet): %v", _err) }()
		return fmt.Errorf("FrameCondition does not process packets")
	case frameInput != nil:
		logger.Tracef(ctx, "SendInput(frame)")
		defer func() { logger.Tracef(ctx, "/SendInput(frame): %v", _err) }()

		if k.Condition != nil && !k.Condition.Match(ctx, *frameInput) {
			var pts int64 = astiav.NoPtsValue
			if frameInput.Frame != nil {
				pts = frameInput.Frame.Pts()
			}
			logger.Tracef(ctx, "frame does not match condition, dropping: stream:%d frame:%p pts:%d", frameInput.StreamIndex, frameInput.Frame, pts)
			return nil
		}

		return k.passthrough(ctx, *frameInput, outputCh)
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
}

func (k *FrameCondition) passthrough(
	ctx context.Context,
	input frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "passthrough")
	defer func() { logger.Tracef(ctx, "/passthrough: %v", _err) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputCh <- packetorframe.OutputUnion{
		Frame: ptr(frame.BuildOutput(
			frame.CloneAsReferenced(input.Frame),
			input.StreamInfo,
		)),
	}:
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
	_ chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
	return nil
}
