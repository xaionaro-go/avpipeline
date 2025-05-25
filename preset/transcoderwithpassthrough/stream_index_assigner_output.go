package transcoderwithpassthrough

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/processor"
	avptypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type streamIndexAssignerOutput[C any, P processor.Abstract] struct {
	StreamForward      *TranscoderWithPassthrough[C, P]
	PreviousResultsMap map[int]int
	AlreadyAssignedMap map[int]struct{}
	Locker             xsync.Mutex
}

func newStreamIndexAssignerOutput[C any, P processor.Abstract](s *TranscoderWithPassthrough[C, P]) *streamIndexAssignerOutput[C, P] {
	return &streamIndexAssignerOutput[C, P]{
		StreamForward:      s,
		PreviousResultsMap: make(map[int]int),
		AlreadyAssignedMap: make(map[int]struct{}),
	}
}

func (s *streamIndexAssignerOutput[C, P]) StreamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) ([]int, error) {
	return xsync.DoA2R2(ctx, &s.Locker, s.streamIndexAssign, ctx, input)
}

func (s *streamIndexAssignerOutput[C, P]) streamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) ([]int, error) {
	switch input.Packet.Source {
	case s.StreamForward.PacketSource, s.StreamForward.inputStreamMapIndicesAsPacketSource, s.StreamForward.MapInputStreamIndicesNode.Processor.Kernel, s.StreamForward.PacketSource, s.StreamForward.NodeStreamFixerPassthrough.MapStreamIndicesNode.Processor.Kernel:
		logger.Tracef(ctx, "passing through index %d as is", input.GetStreamIndex())
		return []int{input.GetStreamIndex()}, nil
	case s.StreamForward.Recoder, s.StreamForward.Recoder.Encoder, s.StreamForward.NodeStreamFixerMain.MapStreamIndicesNode.Processor.Kernel:
		inputStreamIndex := input.GetStreamIndex()
		if v, ok := s.PreviousResultsMap[inputStreamIndex]; ok {
			logger.Debugf(ctx, "reassigning %d as %d (cache)", inputStreamIndex, v)
			return []int{v}, nil
		}

		maxStreamIndex := 0
		s.StreamForward.PacketSource.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			for _, stream := range fmtCtx.Streams() {
				if stream.Index() > maxStreamIndex {
					maxStreamIndex = stream.Index()
				}
			}
		})

		result := maxStreamIndex + 1
		for {
			if _, ok := s.AlreadyAssignedMap[result]; ok {
				result++
				continue
			}
			s.PreviousResultsMap[inputStreamIndex] = result
			s.AlreadyAssignedMap[result] = struct{}{}
			logger.Debugf(ctx, "reassigning %d as %d", inputStreamIndex, result)
			return []int{result}, nil
		}
	default:
		return nil, fmt.Errorf("unexpected source: %T %p %s", input.Packet.Source, input.Packet.Source, input.Packet.Source)
	}
}
