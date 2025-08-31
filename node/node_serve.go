package node

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node/filter"
	framecondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetcondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	extraDebug = false
)

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
		close(*xatomic.SwapPointer(&n.ChangeChanIsServing, ptr(make(chan struct{}))))
		return nil
	}); err != nil {
		sendErr(err)
		return
	}
	defer func() {
		if n.Config.CacheHandler != nil {
			n.Config.CacheHandler.Reset(ctx)
		}
		n.IsServingValue = false
		close(*xatomic.SwapPointer(&n.ChangeChanIsServing, ptr(make(chan struct{}))))
	}()

	procNodeEndCtx := ctx
	for {
		logger.Tracef(ctx, "Serve[%s]: an iteration started", nodeKey)
		pktCh, frameCh := n.Processor.OutputPacketChan(), n.Processor.OutputFrameChan()
		n.updateProcInfo(ctx)
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
		case pkt, ok := <-pktCh:
			if !ok {
				sendErr(io.EOF)
				return
			}
			logger.Tracef(ctx, "pulled from %s a %s packet with stream index %d", n.Processor, pkt.GetMediaType(), pkt.Packet.StreamIndex())
			if n.Config.CacheHandler != nil {
				if err := n.Config.CacheHandler.RememberPacketIfNeeded(ctx, packet.BuildInput(
					pkt.Packet,
					pkt.StreamInfo,
				)); err != nil {
					logger.Errorf(ctx, "unable to cache packet: %v", err)
				}
			}
			n.Locker.Do(ctx, func() {
				pushFurther(
					ctx, n, pkt, n.PushPacketsTos, &serveConfig,
					buildPacketInput,
					getInputPacketFilter,
					getInputPacketChan,
					poolPutPacketInput,
					poolPutPacketOutput,
					getPacketsStats,
				)
			})
		case f, ok := <-frameCh:
			if !ok {
				sendErr(io.EOF)
				return
			}
			n.Locker.Do(ctx, func() {
				pushFurther(
					ctx, n, f, n.PushFramesTos, &serveConfig,
					buildFrameInput,
					getInputFrameFilter,
					getInputFrameChan,
					poolPutFrameInput,
					poolPutFrameOutput,
					getFramesStats,
				)
			})
		}
	}
}

func pushFurther[
	P processor.Abstract,
	I packetorframe.Input, C filter.Condition[I],
	O packetorframe.Output, OP packetorframe.Pointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTos []PushTo[I, C],
	serveConfig *ServeConfig,
	buildInput func(context.Context, *NodeWithCustomData[CD, P], O, PushTo[I, C]) []I,
	getInputCondition func(Abstract) C,
	getPushChan func(processor.Abstract) chan<- I,
	poolPutInput func(I),
	poolPutOutput func(O),
	getFramesOrPacketsStats func(s *types.Counters) *types.CountersSection,
) {
	defer poolPutOutput(outputObj)
	outputObjPtr := OP(&outputObj)

	stats := n.GetCountersPtr()

	if extraDebug {
		ctx = belt.WithField(ctx, "stream_index", outputObjPtr.GetStreamIndex())
	}

	objSize := uint64(outputObjPtr.GetSize())

	mediaType := outputObjPtr.GetMediaType()
	defer func() {
		getFramesOrPacketsStats(stats).Sent.Increment(globaltypes.MediaType(mediaType), objSize)
	}()

	n.updateProcInfoLocked(ctx)

	if len(pushTos) == 0 {
		var zeroValue O
		logger.Debugf(ctx, "nowhere to push to a %T", zeroValue)
		return
	}

	if len(pushTos) == 1 {
		pushTo := pushTos[0]
		n.Locker.ManualUnlock(ctx)
		defer n.Locker.ManualLock(ctx)
		pushToDestination[P, I, C, O, OP, CD](
			ctx,
			n,
			outputObj,
			pushTo,
			serveConfig,
			buildInput,
			getInputCondition,
			getPushChan,
			getFramesOrPacketsStats,
			poolPutInput,
		)
		return
	}

	var wg sync.WaitGroup
	defer func() {
		n.Locker.UDo(ctx, wg.Wait)
	}()
	for _, pushTo := range pushTos {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			pushToDestination[P, I, C, O, OP, CD](
				ctx,
				n,
				outputObj,
				pushTo,
				serveConfig,
				buildInput,
				getInputCondition,
				getPushChan,
				getFramesOrPacketsStats,
				poolPutInput,
			)
		})
	}
}

func pushToDestination[
	P processor.Abstract,
	I packetorframe.Input, C filter.Condition[I],
	O packetorframe.Output, OP packetorframe.Pointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTo PushTo[I, C],
	serveConfig *ServeConfig,
	buildInput func(context.Context, *NodeWithCustomData[CD, P], O, PushTo[I, C]) []I,
	getInputCondition func(Abstract) C,
	getPushChan func(processor.Abstract) chan<- I,
	getFramesOrPacketsStats func(s *types.Counters) *types.CountersSection,
	poolPutInput func(I),
) {
	outputObjPtr := OP(&outputObj)
	mediaType := outputObjPtr.GetMediaType()
	objSize := uint64(outputObjPtr.GetSize())

	for _, inputObj := range buildInput(ctx, n, outputObj, pushTo) {
		filterArg := filter.Input[I]{
			Destination: pushTo.Node,
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
		if pushTo.Node == nil {
			logger.Errorf(ctx, "a nil Node in %s's PushTos", n)
			return
		}

		dstCounters := getFramesOrPacketsStats(pushTo.Node.GetCountersPtr())
		isPushed := false
		defer incrementReceived(dstCounters, &isPushed, globaltypes.MediaType(mediaType), objSize)

		inputCond := getInputCondition(pushTo.Node)
		if any(inputCond) != nil && !inputCond.Match(ctx, filterArg) {
			logger.Tracef(ctx, "input condition %s was not met", inputCond)
			return
		}

		pushChan := getPushChan(pushTo.Node.GetProcessor())
		logger.Tracef(ctx, "pushing to %s %s %T with stream index %d via chan %p", pushTo.Node.GetProcessor(), mediaType, outputObj, outputObjPtr.GetStreamIndex(), pushChan)
		if serveConfig.FrameDrop {
			select {
			case <-ctx.Done():
				return
			case pushChan <- inputObj:
				isPushed = true
			default:
				logger.Errorf(ctx, "unable to push to %s: the queue is full", pushTo.Node)
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
		logger.Tracef(ctx, "pushed to %s %T with stream index %d via chan %p", pushTo.Node.GetProcessor(), outputObj, outputObjPtr.GetStreamIndex(), pushChan)
	}
}

func buildPacketInput[
	P processor.Abstract,
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	pkt packet.Output,
	pushTo PushTo[packet.Input, packetcondition.Condition],
) []packet.Input {
	var result []packet.Input
	if n.Config.CacheHandler != nil {
		pendingPackets, err := n.Config.CacheHandler.GetPendingPackets(ctx, pushTo)
		if err != nil {
			logger.Errorf(ctx, "unable to cache packet: %v", err)
		}
		if len(pendingPackets) > 0 {
			return pendingPackets
		}
	}
	result = append(result, packet.BuildInput(
		packet.CloneAsReferenced(pkt.Packet),
		pkt.StreamInfo,
	))
	return result
}

func buildFrameInput[
	P processor.Abstract,
	CD any,
](
	ctx context.Context,
	_ *NodeWithCustomData[CD, P],
	f frame.Output,
	pushTo PushTo[frame.Input, framecondition.Condition],
) []frame.Input {
	return []frame.Input{frame.BuildInput(
		frame.CloneAsReferenced(f.Frame),
		f.Pos,
		f.StreamInfo,
	)}
}

func getInputPacketFilter(n Abstract) packetcondition.Condition {
	return n.GetInputPacketFilter()
}

func getInputPacketChan(p processor.Abstract) chan<- packet.Input {
	return p.InputPacketChan()
}

func getInputFrameFilter(n Abstract) framecondition.Condition {
	return n.GetInputFrameFilter()
}

func getInputFrameChan(p processor.Abstract) chan<- frame.Input {
	return p.InputFrameChan()
}

func poolPutPacketInput(p packet.Input) {
	packet.Pool.Put(p.Packet)
}

func poolPutPacketOutput(p packet.Output) {
	packet.Pool.Put(p.Packet)
}

func getPacketsStats(s *types.Counters) *types.CountersSection {
	return &s.Packets
}

func poolPutFrameInput(f frame.Input) {
	frame.Pool.Put(f.Frame)
}

func poolPutFrameOutput(f frame.Output) {
	frame.Pool.Put(f.Frame)
}

func getFramesStats(s *types.Counters) *types.CountersSection {
	return &s.Frames
}

func incrementReceived(
	dstCounters *types.CountersSection,
	isPushed *bool,
	mediaType globaltypes.MediaType,
	objSize uint64,
) {
	if *isPushed {
		dstCounters.Received.Increment(mediaType, objSize)
	} else {
		dstCounters.Missed.Increment(mediaType, objSize)
	}
}
