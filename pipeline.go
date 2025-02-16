package avpipeline

import (
	"context"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type ProcessingNode interface {
	io.Closer
	SendPacketChan() chan<- InputPacket
	OutputPacketsChan() <-chan OutputPacket
}

type Pipeline struct {
	ProcessingNode
	PushTo []*Pipeline
}

func NewPipelineNode(processingNode ProcessingNode) *Pipeline {
	return &Pipeline{
		ProcessingNode: processingNode,
	}
}

func (p *Pipeline) Serve(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Serve[%T]", p.ProcessingNode)
	defer func() { logger.Tracef(ctx, "/Serve[%T]: %v", p.ProcessingNode, _err) }()

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	for _, pushTo := range p.PushTo {
		observability.Go(ctx, func() {
			pushTo.Serve(ctx)
		})
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pkt, ok := <-p.ProcessingNode.OutputPacketsChan():
			if !ok {
				return io.EOF
			}

			for _, pushTo := range p.PushTo {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case pushTo.SendPacketChan() <- InputPacket{Packet: ClonePacketAsReferenced(pkt.Packet)}:
				default:
					logger.Errorf(ctx, "unable to push to %T: the queue is full", pushTo.ProcessingNode)
				}
			}

			PacketPool.Put(pkt.Packet)
		}
	}
}
