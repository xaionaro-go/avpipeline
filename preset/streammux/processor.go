package streammux

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

var _ processor.Abstract = (*StreamMux[struct{}])(nil)

func (s *StreamMux[C]) InputChan() chan<- packetorframe.InputUnion {
	return s.InputAll.Node.Processor.InputCh
}
func (s *StreamMux[C]) OutputChan() <-chan packetorframe.OutputUnion {
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
