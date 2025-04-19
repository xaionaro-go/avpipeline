package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
)

type ConditionFilter struct {
	*closeChan
	PacketFilter packetcondition.Condition
	FrameFilter  framecondition.Condition
}

var _ Abstract = (*ConditionFilter)(nil)

func NewConditionFilter(
	packetFilter packetcondition.Condition,
	frameFilter framecondition.Condition,
) *ConditionFilter {
	return &ConditionFilter{
		closeChan:    newCloseChan(),
		PacketFilter: packetFilter,
		FrameFilter:  frameFilter,
	}
}

func (f *ConditionFilter) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) error {
	if f.PacketFilter != nil && !f.PacketFilter.Match(ctx, input) {
		return nil
	}
	outputPacketsCh <- packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.Stream,
		input.Source,
	)
	return nil
}

func (f *ConditionFilter) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if f.FrameFilter != nil && !f.FrameFilter.Match(ctx, input) {
		return nil
	}
	outputFramesCh <- frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.CodecContext,
		input.StreamIndex,
		input.StreamsCount,
		input.StreamDuration,
		input.TimeBase,
		input.Pos,
		input.Duration,
	)
	return nil
}

func (f *ConditionFilter) String() string {
	switch {
	case f.PacketFilter != nil && f.FrameFilter != nil:
		return fmt.Sprintf("ConditionFilter(pkt:%s, frame:%s)", f.PacketFilter, f.FrameFilter)
	case f.PacketFilter != nil:
		return fmt.Sprintf("ConditionFilter(pkt:%s)", f.PacketFilter)
	case f.FrameFilter != nil:
		return fmt.Sprintf("ConditionFilter(frame:%s)", f.PacketFilter)
	default:
		return fmt.Sprintf("ConditionFilter()")
	}
}

func (f *ConditionFilter) Close(ctx context.Context) error {
	f.closeChan.Close(ctx)
	return nil
}

func (f *ConditionFilter) CloseChan() <-chan struct{} {
	return f.closeChan.CloseChan()
}

func (f *ConditionFilter) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}
