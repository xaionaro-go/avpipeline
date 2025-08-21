package streammux

import (
	"context"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ node.Abstract = (*StreamMux[struct{}])(nil)

func (n *StreamMux[C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Tracef(ctx, "StreamMux.Serve(ctx, %s, %p)", cfg, errCh)
	defer logger.Tracef(ctx, "/StreamMux.Serve(ctx, %s, %p)", cfg, errCh)
	n.waitGroup.Add(1)
	defer n.waitGroup.Done()
	startCh := *xatomic.LoadPointer(&n.startedCh)
	select {
	case <-startCh:
		panic("this StreamMux is already serving")
	default:
	}
	close(startCh)
	defer func() {
		xatomic.StorePointer(&n.startedCh, ptr(make(chan struct{})))
	}()
	avpipeline.Serve(ctx, avpipeline.ServeConfig{
		EachNode:             cfg,
		AutoServeNewBranches: true,
	}, errCh, n.InputNode)
}

func (n *StreamMux[C]) String() string {
	return "StreamMux"
}

func (n *StreamMux[C]) IsServing() bool {
	return n.InputNode.IsServing()
}

func (n *StreamMux[C]) GetPushPacketsTos() node.PushPacketsTos {
	return nil
}

func (n *StreamMux[C]) AddPushPacketsTo(
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {
}

func (n *StreamMux[C]) SetPushPacketsTos(
	v node.PushPacketsTos,
) {
}

func (n *StreamMux[C]) GetPushFramesTos() node.PushFramesTos {
	return nil
}

func (n *StreamMux[C]) AddPushFramesTo(
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {
}

func (n *StreamMux[C]) SetPushFramesTos(
	v node.PushFramesTos,
) {
}

func (n *StreamMux[C]) GetProcessor() processor.Abstract {
	return n
}

func (n *StreamMux[C]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return nil
}

func (n *StreamMux[C]) GetChangeChanPushFramesTo() <-chan struct{} {
	return nil
}
