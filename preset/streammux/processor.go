package streammux

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
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
func (s *StreamMux[C]) Flush(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Flush: %v:%p", s, s)
	defer func() { logger.Tracef(ctx, "/Flush: %v:%p: %v", s, s, _err) }()

	var errs []error
	nodes := s.Nodes(ctx)
	for idx, n := range nodes {
		err := n.Flush(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to flush node %q: %w", n.String(), err))
			continue
		}
		_ = idx // for debugger
	}
	return errors.Join(errs...)
}

func (s *StreamMux[C]) CountersPtr() *processortypes.Counters {
	inputCounters := s.Input().GetProcessor().CountersPtr()
	return &processortypes.Counters{
		Processed: globaltypes.CountersSection{
			Packets: inputCounters.Processed.Packets,
			Frames:  inputCounters.Processed.Frames,
		},
		// TODO: add a sum of all outputs as Generated
	}
}
