package streammux

import (
	"context"
	"fmt"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
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
	observability.Go(ctx, func(ctx context.Context) {
		s.inputBitRateMeasurerLoop(ctx)
	})
	rawErrCh := make(chan node.Error, 1)
	defer close(rawErrCh)
	observability.Go(ctx, func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case nodeErr, ok := <-rawErrCh:
				if !ok {
					return
				}
				logger.Tracef(ctx, "got error from rawErrCh: %v", nodeErr)
				if customDataer, ok := nodeErr.Node.(node.GetCustomDataer[OutputCustomData]); ok {
					output := customDataer.GetCustomData().Output
					assert(ctx, output != nil, fmt.Sprintf("<%s> <%T> <%#+v>", nodeErr.Node, nodeErr.Node, nodeErr.Node))
					assert(ctx, s.OutputSwitch != nil)
					if int32(output.ID) != s.OutputSwitch.CurrentValue.Load() {
						logger.Errorf(ctx, "got an error on a non-active output %d: %v, closing it", output.ID, nodeErr.Err)
						delete(s.OutputsMap, output.GetKey())
						output.Close(ctx)
						continue
					}
				}
				select {
				case errCh <- nodeErr:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	avpipeline.Serve(ctx, avpipeline.ServeConfig{
		EachNode:             cfg,
		AutoServeNewBranches: true,
	}, rawErrCh, s.InputNode)
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

func (s *StreamMux[C]) GetChangeChanIsServing() <-chan struct{} {
	return s.InputNode.GetChangeChanIsServing()
}

func (s *StreamMux[C]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return nil
}

func (s *StreamMux[C]) GetChangeChanPushFramesTo() <-chan struct{} {
	return nil
}

func (s *StreamMux[C]) GetChangeChanDrained() <-chan struct{} {
	ctx := context.Background()
	return node.CombineGetChangeChanDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) NotifyInputSent() {
	s.InputNode.NotifyInputSent()
}

func (s *StreamMux[C]) Nodes(ctx context.Context) []node.Abstract {
	nodes := []node.Abstract{
		s.InputNode,
	}
	s.Locker.Do(ctx, func() {
		for _, output := range s.OutputsMap {
			nodes = append(nodes, output.Nodes()...)
		}
	})
	return nodes
}
