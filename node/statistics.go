package node

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

func (stats *Statistics) Convert() ProcessingStatistics {
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

type Statistics struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
	FramesRead      FramesStatistics
	FramesMissed    FramesStatistics
	FramesWrote     FramesStatistics
}

func fromProcessingFramesStats(s ProcessingFramesStatistics) *FramesStatistics {
	stats := &FramesStatistics{}
	stats.Audio.Store(s.Audio)
	stats.Video.Store(s.Video)
	stats.Other.Store(s.Other)
	stats.Unknown.Store(s.Unknown)
	return stats
}

func FromProcessingStatistics(s *ProcessingStatistics) *Statistics {
	stats := &Statistics{}
	stats.BytesCountRead.Store(s.BytesCountRead)
	stats.BytesCountWrote.Store(s.BytesCountWrote)
	stats.FramesRead = *fromProcessingFramesStats(s.FramesRead)
	stats.FramesWrote = *fromProcessingFramesStats(s.FramesWrote)
	return stats
}

func (e *Statistics) GetStats() *ProcessingStatistics {
	return ptr(e.Convert())
}
