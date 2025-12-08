package nodewrapper

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framecondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetcondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type NoServe[N node.Abstract] struct {
	Node N
}

var _ node.Abstract = (*NoServe[node.Abstract])(nil)
var _ node.DotBlockContentStringWriteToer = (*NoServe[node.Abstract])(nil)

func (n *NoServe[N]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Debugf(ctx, "NoServe")
}

func (n *NoServe[N]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(n)
}

func (n *NoServe[N]) OriginalNodeAbstract() node.Abstract {
	orig := n.OriginalNode()
	if reflect.ValueOf(orig).IsZero() {
		return nil
	}
	return orig
}

func (n *NoServe[N]) OriginalNode() N {
	return n.Node
}

func (n *NoServe[N]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if writeToer, ok := any(n.Node).(node.DotBlockContentStringWriteToer); ok {
		writeToer.DotBlockContentStringWriteTo(w, alreadyPrinted)
	}
}

func (n *NoServe[N]) String() string {
	stringer, ok := any(n.Node).(fmt.Stringer)
	if !ok {
		return "NoServe"
	}
	return fmt.Sprintf("NoServe(%s)", stringer)
}

func (n *NoServe[N]) GetPushPacketsTos(
	ctx context.Context,
) node.PushPacketsTos {
	return nil
}

func (n *NoServe[N]) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *node.PushPacketsTos),
) {
}

func (n *NoServe[N]) AddPushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetcondition.Condition,
) {
	panic("NoServe cannot add PushPacketsTo")
}

func (n *NoServe[N]) SetPushPacketsTos(
	ctx context.Context,
	pushTos node.PushPacketsTos,
) {
	panic("NoServe cannot set PushPacketsTos")
}

func (n *NoServe[N]) RemovePushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	panic("NoServe cannot remove PushPacketsTo")
}

func (n *NoServe[N]) GetPushFramesTos(
	ctx context.Context,
) node.PushFramesTos {
	return nil
}

func (n *NoServe[N]) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *node.PushFramesTos),
) {
}

func (n *NoServe[N]) AddPushFramesTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...framecondition.Condition,
) {
	panic("NoServe cannot add PushFramesTo")
}

func (n *NoServe[N]) SetPushFramesTos(
	ctx context.Context,
	pushTos node.PushFramesTos,
) {
	panic("NoServe cannot set PushFramesTos")
}

func (n *NoServe[N]) RemovePushFramesTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	panic("NoServe cannot remove PushFramesTo")
}

func (n *NoServe[N]) IsServing() bool {
	return n.Node.IsServing()
}

func (n *NoServe[N]) GetCountersPtr() *nodetypes.Counters {
	return n.Node.GetCountersPtr()
}

func (n *NoServe[N]) GetProcessor() processor.Abstract {
	return n.Node.GetProcessor()
}

func (n *NoServe[N]) GetInputPacketFilter(
	ctx context.Context,
) packetcondition.Condition {
	return n.Node.GetInputPacketFilter(ctx)
}

func (n *NoServe[N]) SetInputPacketFilter(
	ctx context.Context,
	cond packetcondition.Condition,
) {
	n.Node.SetInputPacketFilter(ctx, cond)
}

func (n *NoServe[N]) GetInputFrameFilter(ctx context.Context) framecondition.Condition {
	return n.Node.GetInputFrameFilter(ctx)
}

func (n *NoServe[N]) SetInputFrameFilter(
	ctx context.Context,
	cond framecondition.Condition,
) {
	n.Node.SetInputFrameFilter(ctx, cond)
}

func (n *NoServe[N]) GetChangeChanIsServing() <-chan struct{} {
	return n.Node.GetChangeChanIsServing()
}

func (n *NoServe[N]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return n.Node.GetChangeChanPushPacketsTo()
}

func (n *NoServe[N]) GetChangeChanPushFramesTo() <-chan struct{} {
	return n.Node.GetChangeChanPushFramesTo()
}

func (n *NoServe[N]) GetChangeChanDrained() <-chan struct{} {
	return n.Node.GetChangeChanDrained()
}

func (n *NoServe[N]) IsDrained(ctx context.Context) bool {
	return n.Node.IsDrained(ctx)
}

func (n *NoServe[N]) Flush(
	ctx context.Context,
) error {
	return n.Node.Flush(ctx)
}
