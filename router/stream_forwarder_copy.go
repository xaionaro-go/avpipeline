package router

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/node"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderCopy struct {
	CancelFunc context.CancelFunc
	Input      *NodeRouting
	Output     node.Abstract
}

var _ StreamForwarder = (*StreamForwarderCopy)(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderCopy(
	ctx context.Context,
	src *NodeRouting,
	dst node.Abstract,
) (_ret *StreamForwarderCopy, _err error) {
	logger.Debugf(ctx, "NewStreamForwarderCopy(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderCopy(%s, %s): %p, %v", src, dst, _ret, _err) }()

	fwd := &StreamForwarderCopy{
		Input:  src,
		Output: dst,
	}
	return fwd, nil
}

func (fwd *StreamForwarderCopy) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return fwd.addPacketsPushing(ctx)
}

func (fwd *StreamForwarderCopy) Source() *NodeRouting {
	return fwd.Input
}

func (fwd *StreamForwarderCopy) Destination() node.Abstract {
	return fwd.Output
}

func (fwd *StreamForwarderCopy) addPacketsPushing(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "addPacketsPushing")
	defer func() { logger.Debugf(ctx, "/addPacketsPushing: %v", _err) }()
	dstNode := fwd.outputAsNode()

	pushTos := fwd.Input.GetPushPacketsTos()
	for _, pushTo := range pushTos {
		if pushTo.Node == dstNode {
			return fmt.Errorf("internal error: packets pushing is already added")
		}
	}

	fwd.Input.AddPushPacketsTo(dstNode)

	err := avpipeline.NotifyAboutPacketSources(ctx, fwd.Input.Processor.Kernel, dstNode)
	if err != nil {
		return fmt.Errorf("internal error: unable to notify about the packet source: %w", err)
	}
	return nil
}

func (fwd *StreamForwarderCopy) removePacketsPushing(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "removePacketsPushing")
	defer func() { logger.Debugf(ctx, "/removePacketsPushing: %v", _err) }()
	return node.RemovePushPacketsTo(ctx, fwd.Input, fwd.outputAsNode())
}

func (fwd *StreamForwarderCopy) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input, fwd.Output)
}

func (fwd *StreamForwarderCopy) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return fwd.removePacketsPushing(ctx)
}

func (fwd *StreamForwarderCopy) outputAsNode() *forwarderCopyOutputAsNode {
	return (*forwarderCopyOutputAsNode)(fwd)
}

type forwarderCopyOutputAsNode StreamForwarderCopy

var _ node.Abstract = (*forwarderCopyOutputAsNode)(nil)

func (fwd *forwarderCopyOutputAsNode) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	fwd.Output.Serve(ctx, cfg, errCh)
}

func (fwd *forwarderCopyOutputAsNode) GetPushPacketsTos() node.PushPacketsTos {
	return fwd.Output.GetPushPacketsTos()
}

func (fwd *forwarderCopyOutputAsNode) AddPushPacketsTo(dst node.Abstract, conds ...packetcondition.Condition) {
	fwd.Output.AddPushPacketsTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	fwd.Output.SetPushPacketsTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode) GetPushFramesTos() node.PushFramesTos {
	return fwd.Output.GetPushFramesTos()
}

func (fwd *forwarderCopyOutputAsNode) AddPushFramesTo(dst node.Abstract, conds ...framecondition.Condition) {
	fwd.Output.AddPushFramesTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode) SetPushFramesTos(pushTos node.PushFramesTos) {
	fwd.Output.SetPushFramesTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode) GetStatistics() *node.NodeStatistics {
	return fwd.Output.GetStatistics()
}

func (fwd *forwarderCopyOutputAsNode) GetProcessor() processor.Abstract {
	return fwd.Output.GetProcessor()
}

func (fwd *forwarderCopyOutputAsNode) GetInputPacketCondition() packetcondition.Condition {
	return fwd.Output.GetInputPacketCondition()
}

func (fwd *forwarderCopyOutputAsNode) SetInputPacketCondition(cond packetcondition.Condition) {
	fwd.Output.SetInputPacketCondition(cond)
}

func (fwd *forwarderCopyOutputAsNode) GetInputFrameCondition() framecondition.Condition {
	return fwd.Output.GetInputFrameCondition()
}

func (fwd *forwarderCopyOutputAsNode) SetInputFrameCondition(cond framecondition.Condition) {
	fwd.Output.SetInputFrameCondition(cond)
}
