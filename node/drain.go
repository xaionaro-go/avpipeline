package node

import (
	"context"
	"sync/atomic"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/logger"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

type ProcessingState struct {
	PendingPackets   int
	PendingFrames    int
	IsProcessorDirty bool
	IsProcessing     bool
	InputSent        atomic.Bool
}

func SetBlockInput(
	ctx context.Context,
	blocked bool,
	node Abstract,
) error {
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
	logger.Tracef(ctx, "CombineIsDrained")
	defer func() { logger.Tracef(ctx, "/CombineIsDrained: %v", _ret) }()
	for _, n := range nodes {
		if !n.IsDrained(ctx) {
			logger.Tracef(ctx, "node %v:%p is not drained", n, n)
			return false
		}
	}
	return true
}

func CombineGetChangeChanDrained(
	ctx context.Context,
	nodes ...Abstract,
) <-chan struct{} {
	if len(nodes) == 0 {
		return nil
	}
	if len(nodes) == 1 {
		logger.Tracef(ctx, "CombineGetChangeChanDrained: single node: %v:%p", nodes[0], nodes[0])
		return nodes[0].GetChangeChanDrained()
	}
	ctx, cancelFn := context.WithCancel(ctx)
	for _, n := range nodes {
		logger.Tracef(ctx, "CombineGetChangeChanDrained: adding node: %v:%p", n, n)
		observability.Go(ctx, func(ctx context.Context) {
			defer cancelFn()
			defer logger.Tracef(ctx, "CombineGetChangeChanDrained: node done: %v:%p", n, n)
			select {
			case <-ctx.Done():
			case <-n.GetChangeChanDrained():
			}
		})
	}
	return ctx.Done()
}

func (n *NodeWithCustomData[C, T]) resetChangeChanDrainedChanNow() {
	xatomic.StorePointer(&n.ChangeChanDrained, ptr(make(chan struct{})))
}

func (n *NodeWithCustomData[C, T]) NotifyInputSent() {
	n.ProcessingState.InputSent.Store(true)
	if n.IsDrainedValue.Swap(false) {
		n.resetChangeChanDrainedChanNow()
	}
}

func (n *NodeWithCustomData[C, T]) GetChangeChanDrained() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanDrained)
}

func (n *NodeWithCustomData[C, T]) IsDrained(ctx context.Context) bool {
	return n.IsDrainedValue.Load()
}

func (n *NodeWithCustomData[C, T]) calculateIfDrained(ctx context.Context) bool {
	return n.ProcessingState.PendingPackets == 0 &&
		n.ProcessingState.PendingFrames == 0 &&
		!n.ProcessingState.InputSent.Load() &&
		!n.ProcessingState.IsProcessing &&
		!n.ProcessingState.IsProcessorDirty
}

func (n *NodeWithCustomData[C, T]) updateProcInfoLocked(
	ctx context.Context,
) {
	proc := n.Processor
	n.ProcessingState.InputSent.Store(false)
	n.ProcessingState.PendingPackets = len(proc.InputPacketChan())
	n.ProcessingState.PendingFrames = len(proc.InputFrameChan())
	if isDirtier, ok := any(n.Processor).(processor.IsDirtier); ok {
		n.ProcessingState.IsProcessorDirty = isDirtier.IsDirty(ctx)
	} else {
		n.ProcessingState.IsProcessorDirty = false
	}
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
