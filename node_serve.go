package avpipeline

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

type ErrNode struct {
	Node *Node
	Err  error
}

func (e ErrNode) Error() string {
	return fmt.Sprintf("received an error on %s: %v", e.Node.Processor, e.Err)
}

func (e ErrNode) Unwrap() error {
	return e.Err
}

func incrementCounters(
	s *FramesStatistics,
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

func (n *Node) Serve(
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- ErrNode,
) {
	ctx = belt.WithField(ctx, "processor", n.Processor.String())
	logger.Tracef(ctx, "Serve")
	defer func() { logger.Tracef(ctx, "/Serve") }()

	sendErr := func(err error) {
		logger.Debugf(ctx, "Serve: sendErr(%v)", err)
		if errCh == nil {
			return
		}
		select {
		case errCh <- ErrNode{
			Node: n,
			Err:  err,
		}:
		default:
			logger.Errorf(ctx, "error queue is full, cannot send error: %v", err)
		}
	}

	defer func() { logger.Debugf(ctx, "finished processing") }()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		logger.Errorf(ctx, "got panic in Node[%s]: %v:\n%s\n", n, r, debug.Stack())
	}()

	procNodeEndCtx := ctx
	for {
		select {
		case <-procNodeEndCtx.Done():
			logger.Debugf(ctx, "initiating closing")
			defer func() { logger.Debugf(ctx, "/closed") }()
			var wg sync.WaitGroup
			defer wg.Wait()
			wg.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				err := n.Processor.Close(ctx)
				if err != nil {
					sendErr(fmt.Errorf("unable to close the processing node: %w", err))
				}
			})
			procNodeEndCtx = context.Background()
		case err := <-n.Processor.ErrorChan():
			if err != nil {
				sendErr(err)
			}
		case pkt, ok := <-n.Processor.OutputPacketsChan():
			if !ok {
				sendErr(io.EOF)
				return
			}
			n.BytesCountWrote.Add(uint64(pkt.Size()))
			logger.Tracef(ctx, "pulled from %s a packet with stream index %d", n.Processor, pkt.Packet.StreamIndex())

			fmtCtx := pkt.FormatContext
			streamIndex := pkt.Packet.StreamIndex()
			assert(ctx, fmtCtx != nil, streamIndex, "fmtCtx != nil", n.String())
			logger.Tracef(ctx, "getOutputStream")
			stream := getOutputStream(
				ctx,
				fmtCtx,
				streamIndex,
			)
			logger.Tracef(ctx, "/getOutputStream: %p", stream)
			assert(ctx, stream != nil, streamIndex, "stream != nil", n.String())
			mediaType := stream.CodecParameters().MediaType()
			incrementCounters(&n.FramesWrote, mediaType)

			for _, pushTo := range n.PushTo {
				pushPkt := types.BuildInputPacket(
					packet.CloneAsReferenced(pkt.Packet),
					fmtCtx,
					stream,
				)
				if pushTo.Condition != nil && !pushTo.Condition.Match(ctx, pushPkt) {
					logger.Tracef(ctx, "push condition %s was not met", pushTo.Condition)
					continue
				}

				dst := pushTo.Node
				if dst.InputCondition != nil && !dst.InputCondition.Match(ctx, pushPkt) {
					logger.Tracef(ctx, "input condition %s was not met", dst.InputCondition)
					continue
				}

				pushChan := dst.Processor.SendInputChan()
				logger.Tracef(ctx, "pushing to %s packet %p with stream index %d via chan %p", dst.Processor, pkt.Packet, pkt.Packet.StreamIndex(), pushChan)
				if serveConfig.FrameDrop {
					select {
					case pushChan <- pushPkt:
					default:
						logger.Errorf(ctx, "unable to push to %s: the queue is full", dst.Processor)
						incrementCounters(&dst.FramesMissed, mediaType)
						packet.Pool.Put(pushPkt.Packet)
						continue
					}
				} else {
					pushChan <- pushPkt
				}
				dst.BytesCountRead.Add(uint64(pkt.Size()))
				incrementCounters(&dst.FramesRead, mediaType)
				logger.Tracef(ctx, "pushed to %s packet %p with stream index %d via chan %p", dst.Processor, pkt.Packet, pkt.Packet.StreamIndex(), pushChan)
			}

			packet.Pool.Put(pkt.Packet)
		}
	}
}
