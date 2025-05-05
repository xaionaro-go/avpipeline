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
	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type ErrNode struct {
	Node AbstractNode
	Err  error
}

func (e ErrNode) Error() string {
	return fmt.Sprintf("received an error on %s: %v", e.Node.GetProcessor(), e.Err)
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

func (n *NodeWithCustomData[C, T]) Serve(
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- ErrNode,
) {
	ctx = belt.WithField(ctx, "processor", n.Processor.String())
	ctx = xsync.WithNoLogging(ctx, true)
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
			logger.Errorf(ctx, "error queue is full, cannot send error: '%v'", err)
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
		case pkt, ok := <-n.Processor.OutputPacketChan():
			if !ok {
				sendErr(io.EOF)
				return
			}
			logger.Tracef(ctx, "pulled from %s a packet with stream index %d", n.Processor, pkt.Packet.StreamIndex())
			n.Locker.Do(ctx, func() {
				pushFurther(
					ctx, n, pkt, n.PushPacketsTos, serveConfig,
					func(
						pkt packet.Output,
					) packet.Input {
						return packet.BuildInput(
							packet.CloneAsReferenced(pkt.Packet),
							pkt.Stream,
							pkt.Source,
						)
					},
					func(n AbstractNode) packetcondition.Condition { return n.GetInputPacketCondition() },
					func(p processor.Abstract) chan<- packet.Input { return p.SendInputPacketChan() },
					func(p packet.Input) { packet.Pool.Put(p.Packet) },
					func(p packet.Output) { packet.Pool.Put(p.Packet) },
				)
			})
		case f, ok := <-n.Processor.OutputFrameChan():
			if !ok {
				sendErr(io.EOF)
				return
			}
			n.Locker.Do(ctx, func() {
				pushFurther(
					ctx, n, f, n.PushFramesTos, serveConfig,
					func(
						f frame.Output,
					) frame.Input {
						return frame.BuildInput(
							frame.CloneAsReferenced(f.Frame),
							f.CodecContext,
							f.StreamIndex, f.StreamsCount,
							f.StreamDuration,
							f.TimeBase,
							f.Pos,
							f.Duration,
						)
					},
					func(n AbstractNode) framecondition.Condition { return n.GetInputFrameCondition() },
					func(p processor.Abstract) chan<- frame.Input { return p.SendInputFrameChan() },
					func(f frame.Input) { frame.Pool.Put(f.Frame) },
					func(f frame.Output) { frame.Pool.Put(f.Frame) },
				)
			})
		}
	}
}

func pushFurther[
	P processor.Abstract,
	I types.InputPacketOrFrame, C types.Condition[I],
	O types.OutputPacketOrFrame, OP types.PacketOrFramePointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTos []PushTo[I, C],
	serveConfig ServeConfig,
	buildInput func(O) I,
	getInputCondition func(AbstractNode) C,
	getPushChan func(processor.Abstract) chan<- I,
	poolPutInput func(I),
	poolPutOutput func(O),
) {
	defer poolPutOutput(outputObj)
	outputObjPtr := OP(ptr(outputObj))

	ctx = belt.WithField(ctx, "stream_index", outputObjPtr.GetStreamIndex())

	objSize := uint64(outputObjPtr.GetSize())
	n.BytesCountWrote.Add(objSize)

	mediaType := outputObjPtr.GetMediaType()
	incrementCounters(&n.FramesWrote, mediaType)

	if len(pushTos) == 0 {
		var zeroValue O
		logger.Debugf(ctx, "nowhere to push to a %T", zeroValue)
		return
	}
	for _, pushTo := range pushTos {
		inputObj := buildInput(outputObj)
		if any(pushTo.Condition) != nil && !pushTo.Condition.Match(ctx, inputObj) {
			logger.Tracef(ctx, "push condition %s was not met", pushTo.Condition)
			continue
		}

		dst := pushTo.Node

		n.Locker.UDo(ctx, func() {
			inputCond := getInputCondition(dst)
			if any(inputCond) != nil && !inputCond.Match(ctx, inputObj) {
				logger.Tracef(ctx, "input condition %s was not met", inputCond)
				return
			}
			dstStats := dst.GetStatistics()

			pushChan := getPushChan(dst.GetProcessor())
			logger.Tracef(ctx, "pushing to %s %T with stream index %d via chan %p", dst.GetProcessor(), outputObj, outputObjPtr.GetStreamIndex(), pushChan)
			if serveConfig.FrameDrop {
				select {
				case pushChan <- inputObj:
				default:
					logger.Errorf(ctx, "unable to push to %s: the queue is full", dst.GetProcessor())
					incrementCounters(&dstStats.FramesMissed, mediaType)
					poolPutInput(inputObj)
					return
				}
			} else {
				pushChan <- inputObj
			}
			dstStats.BytesCountRead.Add(objSize)
			incrementCounters(&dstStats.FramesRead, mediaType)
			logger.Tracef(ctx, "pushed to %s %T with stream index %d via chan %p", dst.GetProcessor(), outputObj, outputObjPtr.GetStreamIndex(), pushChan)
		})
	}
}
