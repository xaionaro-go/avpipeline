package node

import (
	"context"
	"fmt"
	"time"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/logger"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

func SetBlockInput(
	ctx context.Context,
	blocked bool,
	node Abstract,
) (_err error) {
	logger.Debugf(ctx, "SetBlockInput(%v, %p:%p:%v)", blocked, node, node.GetProcessor(), node)
	defer func() {
		logger.Debugf(ctx, "/SetBlockInput(%v, %p:%p:%v): %v", blocked, node, node.GetProcessor(), node, _err)
	}()

	cond := node.GetInputPacketFilter()
	condPause, ok := cond.(*packetfiltercondition.PauseCond)
	if !ok {
		if !blocked {
			return nil
		}
		condPause = packetfiltercondition.Pause(ctx, cond)
		node.SetInputPacketFilter(condPause)
	}
	if blocked {
		return nil
	}
	condPause.ReleasePauseFn()
	node.SetInputPacketFilter(condPause.OriginalCondition)
	return nil
}

func CombineIsDrained(
	ctx context.Context,
	nodes ...Abstract,
) (_ret bool) {
	logger.Tracef(ctx, "CombineIsDrained (nodes: %v)", nodes)
	defer func() { logger.Tracef(ctx, "/CombineIsDrained (nodes: %v): %v", nodes, _ret) }()
	for _, n := range nodes {
		if !n.IsDrained(ctx) {
			logger.Tracef(ctx, "CombineIsDrained: node %v:%p is not drained (%T)", n, n, n)
			return false
		}
	}
	return true
}

func CombineGetChangeChanDrained(
	ctx context.Context,
	nodes ...Abstract,
) <-chan struct{} {
	logger.Tracef(ctx, "CombineGetChangeChanDrained (nodes: %v)", nodes)
	if len(nodes) == 0 {
		panic("no nodes provides")
	}
	if len(nodes) == 1 {
		logger.Tracef(ctx, "CombineGetChangeChanDrained: single node: %v:%p", nodes[0], nodes[0])
		return nodes[0].GetChangeChanDrained()
	}
	ctx, cancelFn := context.WithCancel(ctx)
	for _, n := range nodes {
		logger.Tracef(ctx, "CombineGetChangeChanDrained: adding node: %v:%p", n, n)
		n, ch := n, n.GetChangeChanDrained()
		observability.Go(ctx, func(ctx context.Context) {
			defer cancelFn()
			defer logger.Tracef(ctx, "CombineGetChangeChanDrained: node done: %v:%p", n, n)
			select {
			case <-ctx.Done():
			case <-ch:
			}
			ch = n.GetChangeChanDrained()
		})
	}
	return ctx.Done()
}

func (n *NodeWithCustomData[C, T]) resetChangeChanDrainedChanNow() {
	logger.Tracef(context.Background(), "resetChangeChanDrainedChanNow: %v:%p", n, n)
	close(*xatomic.SwapPointer(&n.ChangeChanDrained, ptr(make(chan struct{}))))
}

func (n *NodeWithCustomData[C, T]) GetChangeChanDrained() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanDrained)
}

func (n *NodeWithCustomData[C, T]) IsDrained(ctx context.Context) bool {
	return n.IsDrainedValue.Load()
}

func allWentInAndOut(
	ctx context.Context,
	nodeCounters *types.Counters,
	procCounters *processortypes.Counters,
) bool {
	for subSectionID := globaltypes.UndefinedSubSectionID + 1; subSectionID < globaltypes.EndOfCountersSubSectionID; subSectionID++ {
		for _, mediaType := range globaltypes.MediaTypes() {

			// the reverse order here is important, it guarantees we cannot falsely claim
			// something is drained due to a race condition:
			sent := nodeCounters.Sent.Get(subSectionID).Get(mediaType).Count.Load()
			generated := procCounters.Generated.Get(subSectionID).Get(mediaType).Count.Load()
			processed := procCounters.Processed.Get(subSectionID).Get(mediaType).Count.Load()
			missed := nodeCounters.Missed.Get(subSectionID).Get(mediaType).Count.Load()
			addressed := nodeCounters.Addressed.Get(subSectionID).Get(mediaType).Count.Load()
			if extraDefensiveChecks {
				assert(ctx, addressed >= processed+missed, addressed, processed)
				assert(ctx, generated >= sent, generated, sent)
			}

			if sent != generated || processed+missed != addressed {
				logger.Tracef(
					ctx,
					"allWentInAndOut: subSectionID=%d mediaType=%s: sent=%#+v; generated=%#+v; processed=%#+v; missed=%#+v; addressed=%#+v",
					subSectionID, mediaType,
					sent, generated, processed, missed, addressed,
				)
				return false
			}

		}
	}

	return true
}

func (n *NodeWithCustomData[C, T]) calculateIfDrained(ctx context.Context) bool {
	if isDirtier, ok := any(n.Processor).(processor.Flusher); ok {
		if isDirtier.IsDirty(ctx) {
			logger.Tracef(ctx, "node %v:%p is dirty", n, n)
			return false
		}
	}

	procCounters := n.Processor.CountersPtr()
	allWentInAndOut := allWentInAndOut(ctx,
		n.Counters,
		procCounters,
	)
	logger.Tracef(ctx, "node %v:%p allWentInAndOut=%v", n, n, allWentInAndOut)
	return allWentInAndOut
}

func (n *NodeWithCustomData[C, T]) updateProcInfoLocked(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateProcInfoLocked: %v:%p", n, n)
	defer func() {
		logger.Tracef(ctx, "/updateProcInfoLocked: %v:%p: isDrained:%v", n, n, n.IsDrainedValue.Load())
	}()

	newIsDrained := n.calculateIfDrained(ctx)
	prevIsDrained := n.IsDrainedValue.Swap(newIsDrained)
	if prevIsDrained != newIsDrained {
		n.resetChangeChanDrainedChanNow()
	}
}

func (n *NodeWithCustomData[C, T]) updateProcInfo(
	ctx context.Context,
) {
	n.Locker.Do(ctx, func() {
		n.updateProcInfoLocked(ctx)
	})
}

func WaitForDrain(
	ctx context.Context,
	node Abstract,
) (_err error) {
	logger.Tracef(ctx, "WaitForDrain: %v:%p", node, node)
	defer func() { logger.Tracef(ctx, "/WaitForDrain: %v:%p: %v", node, node, _err) }()
	for {
		ch := node.GetChangeChanDrained()
		if node.IsDrained(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

func (n *NodeWithCustomData[C, T]) Flush(ctx context.Context) error {
	flusher, ok := any(n.Processor).(processor.Flusher)
	if !ok {
		return nil
	}

	for {
		// this is not required to satisfy "Flush", but we defensively drain the input:
		err := processor.DrainInput(ctx, n.Processor)
		if err != nil {
			return fmt.Errorf("unable to drain input of %v: %w", n, err)
		}

		err = flusher.Flush(ctx)
		if err != nil {
			return fmt.Errorf("unable to flush internal buffers of %v: %w", n, err)
		}

		n.updateProcInfoLocked(ctx)
		if n.IsDrained(ctx) {
			break
		}
		logger.Warnf(ctx, "%v is not drained after flush, retrying", n)
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}
