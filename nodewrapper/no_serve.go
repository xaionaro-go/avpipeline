package nodewrapper

import (
	"context"
	"fmt"
	"io"

	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/node"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
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

func (n *NoServe[T]) GetPushPacketsTos() node.PushPacketsTos {
	return nil
}

func (n *NoServe[T]) AddPushPacketsTo(dst node.Abstract, conds ...packetcondition.Condition) {
	n.Node.AddPushPacketsTo(dst, conds...)
}

func (n *NoServe[T]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	n.Node.SetPushPacketsTos(pushTos)
}

func (n *NoServe[T]) GetPushFramesTos() node.PushFramesTos {
	return nil
}

func (n *NoServe[T]) AddPushFramesTo(dst node.Abstract, conds ...framecondition.Condition) {
	n.Node.AddPushFramesTo(dst, conds...)
}

func (n *NoServe[T]) SetPushFramesTos(pushTos node.PushFramesTos) {
	n.Node.SetPushFramesTos(pushTos)
}

func (n *NoServe[T]) GetStatistics() *node.Statistics {
	return n.Node.GetStatistics()
}

func (n *NoServe[T]) GetProcessor() processor.Abstract {
	return n.Node.GetProcessor()
}

func (n *NoServe[T]) GetInputPacketCondition() packetcondition.Condition {
	return n.Node.GetInputPacketCondition()
}

func (n *NoServe[T]) SetInputPacketCondition(cond packetcondition.Condition) {
	n.Node.SetInputPacketCondition(cond)
}

func (n *NoServe[T]) GetInputFrameCondition() framecondition.Condition {
	return n.Node.GetInputFrameCondition()
}

func (n *NoServe[T]) SetInputFrameCondition(cond framecondition.Condition) {
	n.Node.SetInputFrameCondition(cond)
}
