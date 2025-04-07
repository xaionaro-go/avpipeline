package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
)

type Filter struct {
	*closeChan
	PacketFilter packetcondition.Condition
	FrameFilter  framecondition.Condition
}

var _ Abstract = (*Filter)(nil)

func NewFilter(
	packetFilter packetcondition.Condition,
	frameFilter framecondition.Condition,
) *Filter {
	return &Filter{
		closeChan:    newCloseChan(),
		PacketFilter: packetFilter,
		FrameFilter:  frameFilter,
	}
}

func (f *Filter) SendInputPacket(
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
		input.FormatContext,
	)
	return nil
}

func (f *Filter) SendInputFrame(
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

func (f *Filter) String() string {
	switch {
	case f.PacketFilter != nil && f.FrameFilter != nil:
		return fmt.Sprintf("Filter(pkt:%s, frame:%s)", f.PacketFilter, f.FrameFilter)
	case f.PacketFilter != nil:
		return fmt.Sprintf("Filter(pkt:%s)", f.PacketFilter)
	case f.FrameFilter != nil:
		return fmt.Sprintf("Filter(frame:%s)", f.PacketFilter)
	default:
		return fmt.Sprintf("Filter()")
	}
}

func (f *Filter) Close(ctx context.Context) error {
	f.closeChan.Close(ctx)
	return nil
}

func (f *Filter) CloseChan() <-chan struct{} {
	return f.closeChan.CloseChan()
}

func (f *Filter) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return nil
}
