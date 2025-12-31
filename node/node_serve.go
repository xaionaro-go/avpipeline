package node

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	extraDebug           = true
	extraDefensiveChecks = true

	todoDeleteMeRefreshProcInfoInterval = time.Minute
)

func (n *NodeWithCustomData[C, T]) Serve(
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- Error,
) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	ctx = belt.WithField(ctx, "node_ptr", fmt.Sprintf("%p", n))
	ctx = belt.WithField(ctx, "proc_ptr", fmt.Sprintf("%p", n.GetProcessor()))
	ctx = belt.WithField(ctx, "processor", n.Processor.String())
	ctx = xsync.WithLoggingEnabled(ctx, false)
	nodeKey := fmt.Sprintf("%s:%p:%d", n, n, n.GetObjectID())
	logger.Tracef(ctx, "Serve[%s]: %s", nodeKey, debug.Stack())
	defer func() { logger.Tracef(ctx, "/Serve[%s]", nodeKey) }()

	sendErr := func(err error) {
		logger.Debugf(ctx, "Serve[%s]: sendErr(%v)", nodeKey, err)
		if errCh == nil {
			return
		}
		select {
		case errCh <- Error{
			Node:      n,
			Err:       err,
			DebugData: serveConfig.DebugData,
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
			return ErrAlreadyStarted{
				PreviousDebugData: n.ServeDebugData,
			}
		}
		n.IsServingValue = true
		n.ServeDebugData = serveConfig.DebugData
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
	t := time.NewTicker(todoDeleteMeRefreshProcInfoInterval)
	defer t.Stop()
	for {
		logger.Tracef(ctx, "Serve[%s]: an iteration started", nodeKey)
		outputCh := n.Processor.OutputChan()
		n.updateProcInfo(ctx)
		select {
		case <-t.C:
			// TODO: delete me
			n.updateProcInfo(ctx)
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
		case output, ok := <-outputCh:
			if !ok {
				sendErr(io.EOF)
				return
			}
			if output.Packet != nil {
				pkt := *output.Packet
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
						ctx, n, pkt, n.PushTos, &serveConfig,
						buildPacketInput,
						func(ctx context.Context, n Abstract) packetorframefiltercondition.Condition {
							return n.GetInputFilter(ctx)
						},
						poolPutPacketOutput,
						globaltypes.CountersSubSectionIDPackets,
					)
				})
			}
			if output.Frame != nil {
				f := *output.Frame
				n.Locker.Do(ctx, func() {
					pushFurther(
						ctx, n, f, n.PushTos, &serveConfig,
						buildFrameInput,
						func(ctx context.Context, n Abstract) packetorframefiltercondition.Condition {
							return n.GetInputFilter(ctx)
						},
						poolPutFrameOutput,
						globaltypes.CountersSubSectionIDFrames,
					)
				})
			}
		}
	}
}

func pushFurther[
	P processor.Abstract,
	O packetorframe.Output, OP packetorframe.Pointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTos PushTos,
	serveConfig *ServeConfig,
	buildInput func(context.Context, *NodeWithCustomData[CD, P], O, PushTo) []packetorframe.InputUnion,
	getInputCondition func(context.Context, Abstract) packetorframefiltercondition.Condition,
	poolPutOutput func(O),
	countersSubSectionID globaltypes.CountersSubSectionID,
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
		nodeCountsItem := stats.Sent.Increment(countersSubSectionID, globaltypes.MediaType(mediaType), objSize)
		if extraDebug {
			procCounts := n.Processor.CountersPtr()
			sentCount := nodeCountsItem.Count.Load()
			generatedCount := procCounts.Generated.Get(countersSubSectionID).Get(globaltypes.MediaType(mediaType)).Count.Load()
			omittedCount := procCounts.Omitted.Get(countersSubSectionID).Get(globaltypes.MediaType(mediaType)).Count.Load()
			assertSoft(ctx, sentCount <= generatedCount+omittedCount, fmt.Sprintf("sent more objects than generated, this is a bug: %d > %d+%d (a possible issue: are you pushing data directly to processor's output chan? you should not or at least you should count the generated objects first)", sentCount, generatedCount, omittedCount))
		}
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
		pushToDestination[P, O, OP, CD](
			ctx,
			n,
			outputObj,
			pushTo,
			serveConfig,
			buildInput,
			getInputCondition,
			countersSubSectionID,
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
			pushToDestination[P, O, OP, CD](
				ctx,
				n,
				outputObj,
				pushTo,
				serveConfig,
				buildInput,
				getInputCondition,
				countersSubSectionID,
			)
		})
	}
}

func pushToDestination[
	P processor.Abstract,
	O packetorframe.Output, OP packetorframe.Pointer[O],
	CD any,
](
	ctx context.Context,
	n *NodeWithCustomData[CD, P],
	outputObj O,
	pushTo PushTo,
	serveConfig *ServeConfig,
	buildInput func(context.Context, *NodeWithCustomData[CD, P], O, PushTo) []packetorframe.InputUnion,
	getInputCondition func(context.Context, Abstract) packetorframefiltercondition.Condition,
	countersSubSectionID globaltypes.CountersSubSectionID,
) {
	outputObjPtr := OP(&outputObj)
	mediaType := outputObjPtr.GetMediaType()
	objSize := uint64(outputObjPtr.GetSize())

	for _, inputObj := range buildInput(ctx, n, outputObj, pushTo) {
		filterArg := packetorframefiltercondition.Input{
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

		dstCounters := pushTo.Node.GetCountersPtr()
		isPushed := false
		if extraDefensiveChecks {
			assert(ctx, dstCounters.Addressed.Get(countersSubSectionID).Get(globaltypes.MediaType(mediaType)) != nil, countersSubSectionID, mediaType, objSize)
		}
		dstCounters.Addressed.Increment(countersSubSectionID, globaltypes.MediaType(mediaType), objSize)
		defer incrementReceived(dstCounters, &isPushed, countersSubSectionID, globaltypes.MediaType(mediaType), objSize)

		inputCond := getInputCondition(ctx, pushTo.Node)
		if any(inputCond) != nil && !inputCond.Match(ctx, filterArg) {
			logger.Tracef(ctx, "input condition %s was not met", inputCond)
			return
		}

		p := pushTo.Node.GetProcessor()
		pushChan := p.InputChan()
		isDiscard := pushChan == processor.DiscardInputChan

		logger.Tracef(ctx, "pushing to %s %s %T with stream index %d via chan %p", pushTo.Node.GetProcessor(), mediaType, outputObj, outputObjPtr.GetStreamIndex(), pushChan)
		frameDrop := false
		switch mediaType {
		case astiav.MediaTypeAudio:
			frameDrop = serveConfig.FrameDropAudio
		case astiav.MediaTypeVideo:
			frameDrop = serveConfig.FrameDropVideo
		default:
			frameDrop = serveConfig.FrameDropOther
		}
		switch {
		case isDiscard:
			select {
			case <-ctx.Done():
				return
			default:
				logger.Tracef(ctx, "discarding %s for %s", mediaType, pushTo.Node.GetProcessor())
			}
		case frameDrop:
			select {
			case <-ctx.Done():
				return
			case pushChan <- inputObj:
				isPushed = true
			default:
				logger.Errorf(ctx, "unable to push %s to %s: the %T queue is full (size: %d)", mediaType, pushTo.Node, outputObj, cap(pushChan))
				poolPutInput(inputObj)
				return
			}
		default:
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
	pushTo PushTo,
) []packetorframe.InputUnion {
	var result []packetorframe.InputUnion
	if n.Config.CacheHandler != nil {
		pendingPackets, err := n.Config.CacheHandler.GetPending(ctx, pushTo)
		if err != nil {
			logger.Errorf(ctx, "unable to cache packet: %v", err)
		}
		for _, p := range pendingPackets {
			result = append(result, packetorframe.InputUnion{Packet: &p})
		}
		if len(result) > 0 {
			return result
		}
	}
	p := packet.BuildInput(
		packet.CloneAsReferenced(pkt.Packet),
		pkt.StreamInfo,
	)
	result = append(result, packetorframe.InputUnion{
		Packet: &p,
	})
	return result
}

func buildFrameInput[
	P processor.Abstract,
	CD any,
](
	ctx context.Context,
	_ *NodeWithCustomData[CD, P],
	f frame.Output,
	pushTo PushTo,
) []packetorframe.InputUnion {
	fr := frame.BuildInput(
		frame.CloneAsReferenced(f.Frame),
		f.Pos,
		f.StreamInfo,
	)
	return []packetorframe.InputUnion{{
		Frame: &fr,
	}}
}

func poolPutInput(in packetorframe.InputUnion) {
	if in.Packet != nil {
		packet.Pool.Put(in.Packet.Packet)
	}
	if in.Frame != nil {
		frame.Pool.Put(in.Frame.Frame)
	}
}

func poolPutPacketInput(p packet.Input) {
	packet.Pool.Put(p.Packet)
}

func poolPutPacketOutput(p packet.Output) {
	packet.Pool.Put(p.Packet)
}

func poolPutFrameInput(f frame.Input) {
	frame.Pool.Put(f.Frame)
}

func poolPutFrameOutput(f frame.Output) {
	frame.Pool.Put(f.Frame)
}

func incrementReceived(
	dstCounters *nodetypes.Counters,
	isPushed *bool,
	countersSubSectionID globaltypes.CountersSubSectionID,
	mediaType globaltypes.MediaType,
	objSize uint64,
) {
	if *isPushed {
		dstCounters.Received.Increment(countersSubSectionID, mediaType, objSize)
	} else {
		dstCounters.Missed.Increment(countersSubSectionID, mediaType, objSize)
	}
}
