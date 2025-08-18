package node

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node/filter"
	framecondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetcondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

func incrementCounters(
	s *FramesOrPacketsStatisticsSection,
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
	errCh chan<- Error,
) {
	ctx = belt.WithField(ctx, "node_ptr", fmt.Sprintf("%p", n))
	ctx = belt.WithField(ctx, "proc_ptr", fmt.Sprintf("%p", n.GetProcessor()))
	ctx = belt.WithField(ctx, "processor", n.Processor.String())
	ctx = xsync.WithLoggingEnabled(ctx, false)
	nodeKey := fmt.Sprintf("%s:%p", n, n)
	logger.Tracef(ctx, "Serve[%s]: %s", nodeKey, debug.Stack())
	defer func() { logger.Tracef(ctx, "/Serve[%s]", nodeKey) }()

	sendErr := func(err error) {
		logger.Debugf(ctx, "Serve[%s]: sendErr(%v)", nodeKey, err)
		if errCh == nil {
			return
		}
		select {
		case errCh <- Error{
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
		logger.Errorf(ctx, "got panic in Node[%s]: %v:\n%s\n", nodeKey, r, debug.Stack())
	}()

	if err := xsync.DoR1(ctx, &n.Locker, func() error {
		if n.IsServingValue {
			logger.Debugf(ctx, "double-start: %T: %s", n.CustomData, nodeKey)
			return ErrAlreadyStarted{}
		}
		n.IsServingValue = true
		return nil
	}); err != nil {
		sendErr(err)
		return
	}
	defer func() {
		n.IsServingValue = false
	}()

	procNodeEndCtx := ctx
	for {
		select {
		case <-procNodeEndCtx.Done():
			logger.Debugf(ctx, "Serve[%s]: initiating closing", nodeKey)
			defer func() { logger.Debugf(ctx, "Serve[%s]: /closed", nodeKey) }()
			var wg sync.WaitGroup
			defer wg.Wait()
			wg.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
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
			logger.Tracef(ctx, "pulled from %s a %s packet with stream index %d", n.Processor, pkt.GetMediaType(), pkt.Packet.StreamIndex())
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
					func(n Abstract) packetcondition.Condition { return n.GetInputPacketFilter() },
					func(p processor.Abstract) chan<- packet.Input { return p.SendInputPacketChan() },
					func(p packet.Input) { packet.Pool.Put(p.Packet) },
					func(p packet.Output) { packet.Pool.Put(p.Packet) },
					func(s *Statistics) *FramesOrPacketsStatistics { return &s.Packets },
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
							f.CodecParameters,
							f.StreamIndex, f.StreamsCount,
							f.StreamDuration,
							f.TimeBase,
							f.Pos,
							f.Duration,
						)
					},
					func(n Abstract) framecondition.Condition { return n.GetInputFrameFilter() },
					func(p processor.Abstract) chan<- frame.Input { return p.SendInputFrameChan() },
					func(f frame.Input) { frame.Pool.Put(f.Frame) },
					func(f frame.Output) { frame.Pool.Put(f.Frame) },
					func(s *Statistics) *FramesOrPacketsStatistics { return &s.Frames },
				)
			})
		}
	}
}

func pushFurther[
	P processor.Abstract,
	I types.InputPacketOrFrame, C filter.Condition[I],
	O types.OutputPacketOrFrame, OP types.PacketOrFramePointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTos []PushTo[I, C],
	serveConfig ServeConfig,
	buildInput func(O) I,
	getInputCondition func(Abstract) C,
	getPushChan func(processor.Abstract) chan<- I,
	poolPutInput func(I),
	poolPutOutput func(O),
	getFramesOrPacketsStats func(*Statistics) *FramesOrPacketsStatistics,
) {
	defer poolPutOutput(outputObj)
	outputObjPtr := OP(ptr(outputObj))

	ctx = belt.WithField(ctx, "stream_index", outputObjPtr.GetStreamIndex())

	objSize := uint64(outputObjPtr.GetSize())
	n.BytesCountWrote.Add(objSize)

	mediaType := outputObjPtr.GetMediaType()
	incrementCounters(&getFramesOrPacketsStats(n.Statistics).Wrote, mediaType)

	if len(pushTos) == 0 {
		var zeroValue O
		logger.Debugf(ctx, "nowhere to push to a %T", zeroValue)
		return
	}
	for _, pushTo := range pushTos {
		dst := pushTo.Node
		inputObj := buildInput(outputObj)
		filterArg := filter.Input[I]{
			Destination: dst,
			Input:       inputObj,
		}
		if any(pushTo.Condition) != nil && !pushTo.Condition.Match(ctx, filterArg) {
			logger.Tracef(ctx, "push condition %s was not met", pushTo.Condition)
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
		n.Locker.UDo(ctx, func() {
			if dst == nil {
				logger.Errorf(ctx, "a nil Node in %s's PushTos", n)
				return
			}

			dstStats := dst.GetStatistics()
			dstPacketsOrFramesStats := getFramesOrPacketsStats(dstStats)
			isPushed := false
			defer func() {
				if isPushed {
					dstStats.BytesCountRead.Add(objSize)
					incrementCounters(&dstPacketsOrFramesStats.Read, mediaType)
				} else {
					incrementCounters(&dstPacketsOrFramesStats.Missed, mediaType)
				}
			}()

			inputCond := getInputCondition(dst)
			if any(inputCond) != nil && !inputCond.Match(ctx, filterArg) {
				logger.Tracef(ctx, "input condition %s was not met", inputCond)
				return
			}

			pushChan := getPushChan(dst.GetProcessor())
			logger.Tracef(ctx, "pushing to %s %s %T with stream index %d via chan %p", dst.GetProcessor(), mediaType, outputObj, outputObjPtr.GetStreamIndex(), pushChan)
			if serveConfig.FrameDrop {
				select {
				case <-ctx.Done():
					return
				case pushChan <- inputObj:
					isPushed = true
				default:
					logger.Errorf(ctx, "unable to push to %s: the queue is full", dst)
					poolPutInput(inputObj)
					return
				}
			} else {
				select {
				case <-ctx.Done():
					return
				case pushChan <- inputObj:
					isPushed = true
				}
			}
			logger.Tracef(ctx, "pushed to %s %T with stream index %d via chan %p", dst.GetProcessor(), outputObj, outputObjPtr.GetStreamIndex(), pushChan)
		})
	}
}
