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
type StreamForwarderCopy[CS any, PS processor.Abstract] struct {
	CancelFunc context.CancelFunc
	Input      *node.NodeWithCustomData[CS, PS]
	Output     node.Abstract
}

//var _ StreamForwarder[CS, PS] = (*StreamForwarderCopy[CS, PS])(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderCopy[CS any, PS processor.Abstract](
	ctx context.Context,
	src *node.NodeWithCustomData[CS, PS],
	dst node.Abstract,
) (_ret *StreamForwarderCopy[CS, PS], _err error) {
	logger.Debugf(ctx, "NewStreamForwarderToRouterCopy(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderToRouterCopy(%s, %s): %p, %v", src, dst, _ret, _err) }()

	fwd := &StreamForwarderCopy[CS, PS]{
		Input:  src,
		Output: dst,
	}
	return fwd, nil
}

func (fwd *StreamForwarderCopy[CS, PS]) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return fwd.addPacketsPushing(ctx)
}

func (fwd *StreamForwarderCopy[CS, PS]) addPacketsPushing(
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

	err := avpipeline.NotifyAboutPacketSources(ctx, nil, fwd.Input)
	if err != nil {
		return fmt.Errorf("internal error: unable to notify about the packet source: %w", err)
	}
	return nil
}

func (fwd *StreamForwarderCopy[CS, PS]) removePacketsPushing(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "removePacketsPushing")
	defer func() { logger.Debugf(ctx, "/removePacketsPushing: %v", _err) }()
	return node.RemovePushPacketsTo(ctx, fwd.Input, fwd.outputAsNode())
}

func (fwd *StreamForwarderCopy[CS, PS]) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input, fwd.Output)
}

func (fwd *StreamForwarderCopy[CS, PS]) Source() *node.NodeWithCustomData[CS, PS] {
	return fwd.Input
}

func (fwd *StreamForwarderCopy[CS, PS]) Destination() node.Abstract {
	return fwd.Output
}

func (fwd *StreamForwarderCopy[CS, PS]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return fwd.removePacketsPushing(ctx)
}

func (fwd *StreamForwarderCopy[CS, PS]) outputAsNode() *forwarderCopyOutputAsNode[CS, PS] {
	return (*forwarderCopyOutputAsNode[CS, PS])(fwd)
}

type forwarderCopyOutputAsNode[CS any, PS processor.Abstract] StreamForwarderCopy[CS, PS]

var _ node.Abstract = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)

func (fwd *forwarderCopyOutputAsNode[CS, PS]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	fwd.Output.Serve(ctx, cfg, errCh)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetPushPacketsTos() node.PushPacketsTos {
	return fwd.Output.GetPushPacketsTos()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) AddPushPacketsTo(dst node.Abstract, conds ...packetcondition.Condition) {
	fwd.Output.AddPushPacketsTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	fwd.Output.SetPushPacketsTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetPushFramesTos() node.PushFramesTos {
	return fwd.Output.GetPushFramesTos()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) AddPushFramesTo(dst node.Abstract, conds ...framecondition.Condition) {
	fwd.Output.AddPushFramesTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetPushFramesTos(pushTos node.PushFramesTos) {
	fwd.Output.SetPushFramesTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetStatistics() *node.NodeStatistics {
	return fwd.Output.GetStatistics()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetProcessor() processor.Abstract {
	return fwd.Output.GetProcessor()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetInputPacketCondition() packetcondition.Condition {
	return fwd.Output.GetInputPacketCondition()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetInputPacketCondition(cond packetcondition.Condition) {
	fwd.Output.SetInputPacketCondition(cond)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetInputFrameCondition() framecondition.Condition {
	return fwd.Output.GetInputFrameCondition()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetInputFrameCondition(cond framecondition.Condition) {
	fwd.Output.SetInputFrameCondition(cond)
}
