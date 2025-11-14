package nodewrapper

import (
	"context"
	"fmt"
	"io"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framecondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetcondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type NoServe[T node.Abstract] struct {
	Node T
}

var _ node.Abstract = (*NoServe[node.Abstract])(nil)
var _ node.DotBlockContentStringWriteToer = (*NoServe[node.Abstract])(nil)

func (n *NoServe[T]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Debugf(ctx, "NoServe")
}

func (n *NoServe[T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(n)
}

func (n *NoServe[T]) OriginalNodeAbstract() node.Abstract {
	return n.OriginalNode() // TODO: fix the nil value, it should be untyped
}

func (n *NoServe[T]) OriginalNode() T {
	return n.Node
}

func (n *NoServe[T]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if writeToer, ok := any(n.Node).(node.DotBlockContentStringWriteToer); ok {
		writeToer.DotBlockContentStringWriteTo(w, alreadyPrinted)
	}
}

func (n *NoServe[T]) String() string {
	stringer, ok := any(n.Node).(fmt.Stringer)
	if !ok {
		return "NoServe"
	}
	return fmt.Sprintf("NoServe(%s)", stringer)
}

func (n *NoServe[T]) GetPushPacketsTos(
	ctx context.Context,
) node.PushPacketsTos {
	return n.Node.GetPushPacketsTos(ctx)
}

func (n *NoServe[T]) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *node.PushPacketsTos),
) {
	n.Node.WithPushPacketsTos(ctx, callback)
}

func (n *NoServe[T]) AddPushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetcondition.Condition,
) {
	n.Node.AddPushPacketsTo(ctx, dst, conds...)
}

func (n *NoServe[T]) SetPushPacketsTos(
	ctx context.Context,
	pushTos node.PushPacketsTos,
) {
	n.Node.SetPushPacketsTos(ctx, pushTos)
}

func (n *NoServe[T]) RemovePushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return n.Node.RemovePushPacketsTo(ctx, dst)
}

func (n *NoServe[T]) GetPushFramesTos(
	ctx context.Context,
) node.PushFramesTos {
	return n.Node.GetPushFramesTos(ctx)
}

func (n *NoServe[T]) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *node.PushFramesTos),
) {
	n.Node.WithPushFramesTos(ctx, callback)
}

func (n *NoServe[T]) AddPushFramesTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...framecondition.Condition,
) {
	n.Node.AddPushFramesTo(ctx, dst, conds...)
}

func (n *NoServe[T]) SetPushFramesTos(
	ctx context.Context,
	pushTos node.PushFramesTos,
) {
	n.Node.SetPushFramesTos(ctx, pushTos)
}

func (n *NoServe[T]) RemovePushFramesTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return n.Node.RemovePushFramesTo(ctx, dst)
}

func (n *NoServe[T]) IsServing() bool {
	return n.Node.IsServing()
}

func (n *NoServe[T]) GetCountersPtr() *nodetypes.Counters {
	return n.Node.GetCountersPtr()
}

func (n *NoServe[T]) GetProcessor() processor.Abstract {
	return n.Node.GetProcessor()
}

func (n *NoServe[T]) GetInputPacketFilter() packetcondition.Condition {
	return n.Node.GetInputPacketFilter()
}

func (n *NoServe[T]) SetInputPacketFilter(cond packetcondition.Condition) {
	n.Node.SetInputPacketFilter(cond)
}

func (n *NoServe[T]) GetInputFrameFilter() framecondition.Condition {
	return n.Node.GetInputFrameFilter()
}

func (n *NoServe[T]) SetInputFrameFilter(cond framecondition.Condition) {
	n.Node.SetInputFrameFilter(cond)
}

func (n *NoServe[T]) GetChangeChanIsServing() <-chan struct{} {
	return n.Node.GetChangeChanIsServing()
}

func (n *NoServe[T]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return n.Node.GetChangeChanPushPacketsTo()
}

func (n *NoServe[T]) GetChangeChanPushFramesTo() <-chan struct{} {
	return n.Node.GetChangeChanPushFramesTo()
}

func (n *NoServe[T]) GetChangeChanDrained() <-chan struct{} {
	return n.Node.GetChangeChanDrained()
}

func (n *NoServe[T]) IsDrained(ctx context.Context) bool {
	return n.Node.IsDrained(ctx)
}

func (n *NoServe[T]) Flush(
	ctx context.Context,
) error {
	return n.Node.Flush(ctx)
}
