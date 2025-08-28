package streammux

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*StreamMux[struct{}])(nil)

func (s *StreamMux[C]) InputPacketChan() chan<- packet.Input {
	return s.InputNode.Processor.InputPacketCh
}
func (s *StreamMux[C]) OutputPacketChan() <-chan packet.Output {
	return nil
}
func (s *StreamMux[C]) InputFrameChan() chan<- frame.Input {
	return s.InputNode.Processor.InputFrameCh
}
func (s *StreamMux[C]) OutputFrameChan() <-chan frame.Output {
	return nil
}
func (s *StreamMux[C]) ErrorChan() <-chan error {
	panic("not implemented")
}
func (s *StreamMux[C]) Flush(ctx context.Context) error {
	var errs []error
	nodes := s.Nodes(ctx)
	for idx, n := range nodes {
		err := n.GetProcessor().Flush(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to flush node %q: %w", n.String(), err))
			continue
		}
		for {
			logger.Tracef(ctx, "waiting for drain of %v:%p", n, n)
			ch := n.GetChangeChanDrained()
			if n.IsDrained(ctx) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ch:
			}
		}
		_ = idx // for debugger
	}
	return errors.Join(errs...)
}
