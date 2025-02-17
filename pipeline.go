package avpipeline

import (
	"context"
	"io"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type ProcessingNode interface {
	io.Closer
	GetOutputFormatContext(ctx context.Context) *astiav.FormatContext
	SendPacketChan() chan<- InputPacket
	OutputPacketsChan() <-chan OutputPacket
}

type Pipeline struct {
	CommonsProcessing
	ProcessingNode
	PushTo []*Pipeline
}

func NewPipelineNode(processingNode ProcessingNode) *Pipeline {
	return &Pipeline{
		ProcessingNode: processingNode,
	}
}

func getOutputStream(fmtCtx *astiav.FormatContext, streamIndex int) *astiav.Stream {
	for _, stream := range fmtCtx.Streams() {
		if stream.Index() == streamIndex {
			return stream
		}
	}
	return nil
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
			p.BytesCountWrote.Add(uint64(pkt.Size()))

			fmtCtx := p.ProcessingNode.GetOutputFormatContext(ctx)
			stream := getOutputStream(
				fmtCtx,
				pkt.Packet.StreamIndex(),
			)
			switch stream.CodecParameters().MediaType() {
			case astiav.MediaTypeVideo:
				p.FramesWrote.Video.Add(1)
			case astiav.MediaTypeAudio:
				p.FramesWrote.Audio.Add(1)
			default:
				p.FramesWrote.Other.Add(1)
			}

			for _, pushTo := range p.PushTo {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case pushTo.SendPacketChan() <- InputPacket{
					Packet:        ClonePacketAsReferenced(pkt.Packet),
					Stream:        stream,
					FormatContext: fmtCtx,
				}:
					pushTo.BytesCountRead.Add(uint64(pkt.Size()))
					switch stream.CodecParameters().MediaType() {
					case astiav.MediaTypeVideo:
						pushTo.FramesRead.Video.Add(1)
					case astiav.MediaTypeAudio:
						pushTo.FramesRead.Audio.Add(1)
					default:
						pushTo.FramesRead.Other.Add(1)
					}
				default:
					logger.Errorf(ctx, "unable to push to %T: the queue is full", pushTo.ProcessingNode)
				}
			}

			PacketPool.Put(pkt.Packet)
		}
	}
}
