package avpipeline

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

type ErrPipeline struct {
	Node *Pipeline
	Err  error
}

func (e ErrPipeline) Error() string {
	return fmt.Sprintf("received an error on %s: %v", e.Node.ProcessingNode, e.Err)
}

func (e ErrPipeline) Unwrap() error {
	return e.Err
}

func incrementCounters(
	s *CommonsProcessingFramesStatistics,
	mediaType astiav.MediaType,
) {
	switch mediaType {
	case astiav.MediaTypeVideo:
		s.Video.Add(1)
	case astiav.MediaTypeAudio:
		s.Audio.Add(1)
	default:
		s.Other.Add(1)
	}
}

func (p *Pipeline) Serve(
	ctx context.Context,
	serveConfig PipelineServeConfig,
	errCh chan<- ErrPipeline,
) {
	logger.Tracef(ctx, "Serve[%T]", p.ProcessingNode)
	defer func() { logger.Tracef(ctx, "/Serve[%T]", p.ProcessingNode) }()

	childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
	var wg sync.WaitGroup
	for _, pushTo := range p.PushTo {
		pushTo := pushTo
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			pushTo.Serve(childrenCtx, serveConfig, errCh)
		})
	}
	defer wg.Wait()
	defer childrenCancelFn()

	sendErr := func(err error) {
		logger.Debugf(ctx, "Serve[%T]: sendErr(%v)", p.ProcessingNode, err)
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

	defer func() { logger.Debugf(ctx, "Serve[%T]: finished processing", p.ProcessingNode) }()
	defer func() { errmon.ObserveRecoverCtx(ctx, recover()) }()

	procNodeEndCtx := ctx
	for {
		select {
		case <-procNodeEndCtx.Done():
			logger.Debugf(ctx, "Serve[%T]: initiating closing", p.ProcessingNode)
			defer func() { logger.Debugf(ctx, "Serve[%T]: /closed", p.ProcessingNode) }()
			var wg sync.WaitGroup
			defer wg.Wait()
			wg.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				err := p.ProcessingNode.Close()
				if err != nil {
					sendErr(fmt.Errorf("unable to close the processing node: %w", err))
				}
			})
			procNodeEndCtx = context.Background()
		case err := <-p.ProcessingNode.ErrorChan():
			if err != nil {
				sendErr(err)
			}
		case pkt, ok := <-p.ProcessingNode.OutputPacketsChan():
			if !ok {
				sendErr(io.EOF)
				return
			}
			p.BytesCountWrote.Add(uint64(pkt.Size()))
			logger.Tracef(ctx, "pulled from %s a packet with stream index %d", p.ProcessingNode, pkt.Packet.StreamIndex())

			fmtCtx := p.ProcessingNode.GetOutputFormatContext(ctx)
			streamIndex := pkt.Packet.StreamIndex()
			assert(ctx, fmtCtx != nil, streamIndex, "fmtCtx != nil", p.String())
			logger.Tracef(ctx, "Serve[%T]: getOutputStream", p.ProcessingNode)
			stream := getOutputStream(
				ctx,
				fmtCtx,
				streamIndex,
			)
			logger.Tracef(ctx, "Serve[%T]: /getOutputStream: %p", p.ProcessingNode, stream)
			assert(ctx, stream != nil, streamIndex, "stream != nil", p.String())
			mediaType := stream.CodecParameters().MediaType()
			incrementCounters(&p.FramesWrote, mediaType)

			for _, pushTo := range p.PushTo {
				pushPkt := InputPacket{
					Packet:        ClonePacketAsReferenced(pkt.Packet),
					Stream:        stream,
					FormatContext: fmtCtx,
				}
				if pushTo.Condition != nil && !pushTo.Condition.Match(ctx, pushPkt) {
					logger.Tracef(ctx, "condition %s was not met", pushTo.Condition)
					continue
				}

				dst := pushTo.Pipeline
				pushChan := dst.SendPacketChan()
				logger.Tracef(ctx, "pushing to %s packet %p with stream index %d via chan %p", dst.ProcessingNode, pkt.Packet, pkt.Packet.StreamIndex(), pushChan)

				if serveConfig.FrameDrop {
					select {
					case pushChan <- pushPkt:
					default:
						logger.Errorf(ctx, "unable to push to %s: the queue is full", dst.ProcessingNode)
						incrementCounters(&dst.FramesMissed, mediaType)
						PacketPool.Put(pushPkt.Packet)
						continue
					}
				} else {
					pushChan <- pushPkt
				}
				dst.BytesCountRead.Add(uint64(pkt.Size()))
				incrementCounters(&dst.FramesRead, mediaType)
				logger.Tracef(ctx, "pushed to %s packet %p with stream index %d via chan %p", dst.ProcessingNode, pkt.Packet, pkt.Packet.StreamIndex(), pushChan)
			}

			PacketPool.Put(pkt.Packet)
		}
	}
}
