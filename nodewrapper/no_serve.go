package nodewrapper

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
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

func (n *NoServe[N]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return nil
}

func (n *NoServe[N]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
}

func (n *NoServe[N]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	panic("NoServe cannot add PushTo")
}

func (n *NoServe[N]) SetPushTos(
	ctx context.Context,
	pushTos node.PushTos,
) {
	panic("NoServe cannot set PushTos")
}

func (n *NoServe[N]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	panic("NoServe cannot remove PushTo")
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

func (n *NoServe[N]) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	return n.Node.GetInputFilter(ctx)
}

func (n *NoServe[N]) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	n.Node.SetInputFilter(ctx, cond)
}

func (n *NoServe[N]) GetChangeChanIsServing() <-chan struct{} {
	return n.Node.GetChangeChanIsServing()
}

func (n *NoServe[N]) GetChangeChanPushTo() <-chan struct{} {
	return n.Node.GetChangeChanPushTo()
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
