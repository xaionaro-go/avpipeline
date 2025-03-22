package avpipeline

import (
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/types"
)

type ProcessingFramesStatistics = types.ProcessingFramesStatistics
type ProcessingStatistics = types.ProcessingStatistics

type FramesStatistics struct {
	Unknown atomic.Uint64
	Other   atomic.Uint64
	Video   atomic.Uint64
	Audio   atomic.Uint64
}

func (stats *NodeStatistics) Convert() ProcessingStatistics {
	return ProcessingStatistics{
		BytesCountRead:  stats.BytesCountRead.Load(),
		BytesCountWrote: stats.BytesCountWrote.Load(),
		FramesRead: ProcessingFramesStatistics{
			Unknown: stats.FramesRead.Unknown.Load(),
			Other:   stats.FramesRead.Other.Load(),
			Video:   stats.FramesRead.Video.Load(),
			Audio:   stats.FramesRead.Audio.Load(),
		},
		FramesWrote: ProcessingFramesStatistics{
			Unknown: stats.FramesWrote.Unknown.Load(),
			Other:   stats.FramesWrote.Other.Load(),
			Video:   stats.FramesWrote.Video.Load(),
			Audio:   stats.FramesWrote.Audio.Load(),
		},
	}
}

type NodeStatistics struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
	FramesRead      FramesStatistics
	FramesMissed    FramesStatistics
	FramesWrote     FramesStatistics
}

func (e *NodeStatistics) GetStats() *ProcessingStatistics {
	return ptr(e.Convert())
}
