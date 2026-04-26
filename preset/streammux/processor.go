// processor.go implements the processor interface for the stream muxer.

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
	// StreamMux does not have a single error channel; errors are propagated
	// through individual output nodes. Returning nil blocks any receiver,
	// which is the correct behavior for "no errors from this source".
	return nil
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

// addCountersSubSection accumulates src into dst by atomically adding loaded
// values from src to dst. Used by CountersPtr to sum per-output counters.
func addCountersSubSection(dst, src *globaltypes.CountersSubSection) {
	dst.Video.Count.Add(src.Video.Count.Load())
	dst.Video.Bytes.Add(src.Video.Bytes.Load())
	dst.Audio.Count.Add(src.Audio.Count.Load())
	dst.Audio.Bytes.Add(src.Audio.Bytes.Load())
	dst.Other.Count.Add(src.Other.Count.Load())
	dst.Other.Bytes.Add(src.Other.Bytes.Load())
	dst.Unknown.Count.Add(src.Unknown.Count.Load())
	dst.Unknown.Bytes.Add(src.Unknown.Bytes.Load())
}

// addCountersSection adds src into dst for both Packets and Frames.
func addCountersSection(dst, src *globaltypes.CountersSection) {
	addCountersSubSection(&dst.Packets, &src.Packets)
	addCountersSubSection(&dst.Frames, &src.Frames)
}

func (s *StreamMux[C]) CountersPtr() *processortypes.Counters {
	inputCounters := s.Input().GetProcessor().CountersPtr()
	out := &processortypes.Counters{
		Processed: globaltypes.CountersSection{
			Packets: inputCounters.Processed.Packets,
			Frames:  inputCounters.Processed.Frames,
		},
		Generated: globaltypes.NewCountersSection(),
		Omitted:   globaltypes.NewCountersSection(),
	}
	s.OutputsMap.Range(func(_ SenderKey, output *Output[C]) bool {
		outProc := output.SendingNode.GetProcessor().CountersPtr()
		// SendingNode is a sink: its Processed is what was received from upstream.
		// From StreamMux's perspective that's what we "generated" for downstream consumption.
		addCountersSection(&out.Generated, &outProc.Processed)
		return true
	})
	return out
}
