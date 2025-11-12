package streammux

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type streamIndexAssigner struct {
	MuxMode      types.MuxMode
	OutputID     OutputID
	PacketSource packet.Source
}

func newStreamIndexAssigner(
	muxMode types.MuxMode,
	outputID OutputID,
	packetSource packet.Source,
) *streamIndexAssigner {
	return &streamIndexAssigner{
		MuxMode:      muxMode,
		OutputID:     outputID,
		PacketSource: packetSource,
	}
}

func (s *streamIndexAssigner) StreamIndexAssign(
	ctx context.Context,
	in packetorframe.InputUnion,
) ([]int, error) {
	switch s.MuxMode {
	case types.MuxModeForbid,
		types.MuxModeSameOutputSameTracks,
		types.MuxModeDifferentOutputsSameTracks,
		types.MuxModeDifferentOutputsSameTracksSplitAV:
		return []int{in.GetStreamIndex()}, nil
	case types.MuxModeSameOutputDifferentTracks:
		if s.OutputID == 0 {
			return []int{in.GetStreamIndex()}, nil
		}
		var streamCount int
		s.PacketSource.WithOutputFormatContext(ctx, func(formatCtx *astiav.FormatContext) {
			streamCount = formatCtx.NbStreams()
		})
		if streamCount <= 0 {
			return nil, fmt.Errorf("no streams in the output format context")
		}
		return []int{in.GetStreamIndex() + int(s.OutputID)*streamCount}, nil
	default:
		return nil, fmt.Errorf("unknown MuxMode: %s", s.MuxMode)
	}
}
