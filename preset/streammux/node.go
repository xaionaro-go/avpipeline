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

func (s *StreamMux[C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Tracef(ctx, "StreamMux.Serve(ctx, %s, %p)", cfg, errCh)
	defer logger.Tracef(ctx, "/StreamMux.Serve(ctx, %s, %p)", cfg, errCh)
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	startCh := *xatomic.LoadPointer(&s.startedCh)
	select {
	case <-startCh:
		panic("this StreamMux is already serving")
	default:
	}
	close(startCh)
	defer func() {
		xatomic.StorePointer(&s.startedCh, ptr(make(chan struct{})))
	}()
	avpipeline.Serve(ctx, avpipeline.ServeConfig{
		EachNode:             cfg,
		AutoServeNewBranches: true,
	}, errCh, s.InputNode)
}

func (s *StreamMux[C]) String() string {
	return "StreamMux"
}

func (s *StreamMux[C]) IsServing() bool {
	return s.InputNode.IsServing()
}

func (s *StreamMux[C]) GetPushPacketsTos() node.PushPacketsTos {
	return nil
}

func (s *StreamMux[C]) AddPushPacketsTo(
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {
}

func (s *StreamMux[C]) SetPushPacketsTos(
	v node.PushPacketsTos,
) {
}

func (s *StreamMux[C]) GetPushFramesTos() node.PushFramesTos {
	return nil
}

func (s *StreamMux[C]) AddPushFramesTo(
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {
}

func (s *StreamMux[C]) SetPushFramesTos(
	v node.PushFramesTos,
) {
}

func (s *StreamMux[C]) GetProcessor() processor.Abstract {
	return s
}

func (s *StreamMux[C]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return nil
}

func (s *StreamMux[C]) GetChangeChanPushFramesTo() <-chan struct{} {
	return nil
}
