package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*StreamMux[struct{}])(nil)

func (s *StreamMux[C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Tracef(ctx, "StreamMux.Serve(ctx, %#+v, %p)", cfg, errCh)
	defer logger.Tracef(ctx, "/StreamMux.Serve(ctx, %#+v, %p)", cfg, errCh)
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
	observability.Go(ctx, func(ctx context.Context) {
		s.latencyMeasurerLoop(ctx)
	})
	rawErrCh := make(chan node.Error, 100)
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
				if customDataer, ok := nodeErr.Node.(node.GetCustomDataer[OutputCustomData[C]]); ok {
					output := customDataer.GetCustomData().Output
					assert(ctx, output != nil, fmt.Sprintf("<%s> <%T> <%#+v>", nodeErr.Node, nodeErr.Node, nodeErr.Node))
					err := s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
						if int32(output.ID) == input.OutputSwitch.CurrentValue.Load() {
							logger.Errorf(ctx, "error from the active output %d (%s) of input %s, node %T:%s: %v", output.ID, output.GetKey(), input.GetType(), nodeErr.Node, nodeErr.Node, nodeErr.Err)
							return nodeErr.Err
						}
						switch {
						case errors.Is(nodeErr.Err, io.EOF):
							logger.Debugf(ctx, "node <%T> received EOF, closing it", nodeErr.Node)
						case errors.Is(nodeErr.Err, context.Canceled):
							logger.Debugf(ctx, "node <%T> was canceled, closing it", nodeErr.Node)
						default:
							if r := processPlatformSpecificError(ctx, nodeErr.Err); r == nil {
								logger.Debugf(ctx, "got a platform-specific non-active output error %d: %v, closing it", output.ID, nodeErr.Err)
							} else {
								logger.Errorf(ctx, "got an error on a non-active output %d: %v, closing it", output.ID, nodeErr.Err)
							}
						}
						if s.OutputsMap.CompareAndDelete(output.GetKey(), output) {
							logger.Debugf(ctx, "output %d removed from OutputsMap", output.ID)
						}
						if err := output.CloseNoDrain(ctx); err != nil {
							logger.Debugf(ctx, "unable to close output %d: %v", output.ID, err)
						}
						return nil
					})
					if err == nil {
						// the error was handled
						continue
					}
				} else {
					err := nodeErr.Err
					if h, ok := nodeErr.Node.GetProcessor().(globaltypes.ErrorHandler); ok {
						err = h.HandleError(ctx, nodeErr.Err)
					}
					if err == nil {
						logger.Debugf(ctx, "error from node %T:%s was 4r", nodeErr.Node, nodeErr.Node)
						// the error was handled
						continue
					}
					logger.Errorf(ctx, "node %T:%s does not implement GetCustomDataer[OutputCustomData]; do not know how to handle error %v (%v)", nodeErr.Node, nodeErr.Node, nodeErr.Err, err)
				}
				logger.Debugf(ctx, "forwarding error from node %T:%s to errCh: %v", nodeErr.Node, nodeErr.Node, nodeErr.Err)
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
	}, rawErrCh, s.InputAll.Node)
}

func (s *StreamMux[C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(s)
}

func (s *StreamMux[C]) String() string {
	return "StreamMux"
}

func (s *StreamMux[C]) IsServing() bool {
	return s.InputAll.Node.IsServing()
}

func (n *StreamMux[C]) OriginalNodeAbstract() node.Abstract {
	origN := n.OriginalNode()
	if origN == nil {
		return nil
	}
	return origN
}

func (n *StreamMux[C]) OriginalNode() *NodeInput[C] {
	return n.InputAll.Node
}

func (s *StreamMux[C]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return nil
}

func (a *StreamMux[C]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
}

func (s *StreamMux[C]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
}

func (s *StreamMux[C]) SetPushTos(
	ctx context.Context,
	v node.PushTos,
) {
}

func (s *StreamMux[C]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return nil
}

func (s *StreamMux[C]) GetProcessor() processor.Abstract {
	return s
}

func (s *StreamMux[C]) GetChangeChanIsServing() <-chan struct{} {
	return s.InputAll.Node.GetChangeChanIsServing()
}

func (s *StreamMux[C]) GetChangeChanPushTo() <-chan struct{} {
	return nil
}

func (s *StreamMux[C]) GetChangeChanDrained() <-chan struct{} {
	ctx := context.Background()
	return node.CombineGetChangeChanDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) Nodes(ctx context.Context) []node.Abstract {
	nodes := []node.Abstract{
		s.InputAll.Node,
	}
	s.Locker.Do(ctx, func() {
		s.OutputsMap.Range(func(_ SenderKey, output *Output[C]) bool {
			nodes = append(nodes, output.Nodes()...)
			return true
		})
	})
	return nodes
}

func (s *StreamMux[C]) GetCountersPtr() *nodetypes.Counters {
	inputStats := s.InputAll.Node.GetCountersPtr()

	return &nodetypes.Counters{
		Addressed: inputStats.Addressed,
		Missed:    inputStats.Missed,
		Received:  inputStats.Received,
		// TODO: add sum of all outputs as Sent
	}
}
