package avpipeline

import (
	"context"
	"fmt"
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
	ErrorChan() <-chan error
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

type ErrPipeline struct {
	Node *Pipeline
	Err  error
}

func (e ErrPipeline) Error() string {
	return fmt.Sprintf("received an error on %T: %v", e.Node, e.Err)
}

func (e ErrPipeline) Unwrap() error {
	return e.Err
}

func (p *Pipeline) Serve(
	ctx context.Context,
	errCh chan<- ErrPipeline,
) {
	logger.Tracef(ctx, "Serve[%T]", p.ProcessingNode)
	defer func() { logger.Tracef(ctx, "/Serve[%T]", p.ProcessingNode) }()

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	for _, pushTo := range p.PushTo {
		pushTo := pushTo
		observability.Go(ctx, func() {
			pushTo.Serve(ctx, errCh)
		})
	}

	sendErr := func(err error) {
		logger.Debugf(ctx, "sendErr(%v)", err)
		if errCh == nil {
			return
		}
		select {
		case errCh <- ErrPipeline{
			Node: p,
			Err:  err,
		}:
		default:
			logger.Errorf(ctx, "error queue is full, cannot send error: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			sendErr(ctx.Err())
			return
		case err := <-p.ProcessingNode.ErrorChan():
			sendErr(err)
			return
		case pkt, ok := <-p.ProcessingNode.OutputPacketsChan():
			if !ok {
				sendErr(io.EOF)
				return
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
					sendErr(ctx.Err())
					return
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
