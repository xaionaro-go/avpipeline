package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type Wait struct {
	*closeChan
	Locker             xsync.Mutex
	PacketCondition    packetcondition.Condition
	FrameCondition     framecondition.Condition
	PacketsQueue       []packet.Output
	FramesQueue        []frame.Output
	MaxPacketQueueSize uint
	MaxFrameQueueSize  uint
}

var _ Abstract = (*Wait)(nil)

func NewWait(
	packetCondition packetcondition.Condition,
	frameCondition framecondition.Condition,
	maxPacketQueueSize uint,
	maxFrameQueueSize uint,
) *Wait {
	m := &Wait{
		closeChan:          newCloseChan(),
		PacketCondition:    packetCondition,
		FrameCondition:     frameCondition,
		MaxPacketQueueSize: maxPacketQueueSize,
		MaxFrameQueueSize:  maxFrameQueueSize,
	}
	return m
}

func (w *Wait) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) error {
	return xsync.DoA3R1(ctx, &w.Locker, w.sendInputPacket, ctx, input, outputPacketsCh)
}

func (w *Wait) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
) error {
	output := packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.Stream,
		input.Source,
	)
	shouldWait := w.PacketCondition != nil && w.PacketCondition.Match(ctx, input)
	logger.Tracef(ctx, "shouldWait: %t", shouldWait)
	if shouldWait {
		w.PacketsQueue = append(w.PacketsQueue, output)
		if len(w.PacketsQueue) > int(w.MaxPacketQueueSize) {
			w.PacketsQueue = w.PacketsQueue[1:]
		}
		return nil
	}

	if len(w.PacketsQueue) > 0 {
		logger.Debugf(ctx, "releasing %d packets", len(w.PacketsQueue))
		for _, pkt := range w.PacketsQueue {
			outputPacketsCh <- pkt
		}
		w.PacketsQueue = w.PacketsQueue[:0]
	}

	outputPacketsCh <- output
	return nil
}

func (w *Wait) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA3R1(ctx, &w.Locker, w.sendInputFrame, ctx, input, outputFramesCh)
}

func (w *Wait) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputFramesCh chan<- frame.Output,
) error {
	output := frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.CodecParameters,
		input.StreamIndex,
		input.StreamsCount,
		input.StreamDuration,
		input.TimeBase,
		input.Pos,
		input.Duration,
	)
	shouldWait := w.FrameCondition != nil && w.FrameCondition.Match(ctx, input)
	logger.Tracef(ctx, "shouldWait: %t", shouldWait)
	if shouldWait {
		w.FramesQueue = append(w.FramesQueue, output)
		if len(w.FramesQueue) > int(w.MaxFrameQueueSize) {
			w.FramesQueue = w.FramesQueue[1:]
		}
		return nil
	}

	if len(w.FramesQueue) > 0 {
		logger.Debugf(ctx, "releasing %d frames", len(w.FramesQueue))
		for _, pkt := range w.FramesQueue {
			outputFramesCh <- pkt
		}
	}

	outputFramesCh <- output
	return nil
}

func (w *Wait) String() string {
	return fmt.Sprintf("Wait(%v, %v)", w.PacketCondition, w.FrameCondition)
}

func (w *Wait) Close(ctx context.Context) error {
	w.closeChan.Close(ctx)
	return nil
}

func (w *Wait) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return nil
}
