package avpipeline

import (
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/types"
)

type ProcessingFramesStatistics = types.ProcessingFramesStatistics
type ProcessingStatistics = types.ProcessingStatistics

type CommonsProcessingFramesStatistics struct {
	Unknown atomic.Uint64
	Other   atomic.Uint64
	Video   atomic.Uint64
	Audio   atomic.Uint64
}

type CommonsProcessingStatistics struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
	FramesRead      CommonsProcessingFramesStatistics
	FramesWrote     CommonsProcessingFramesStatistics
}

func (stats *CommonsProcessingStatistics) Convert() ProcessingStatistics {
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

type CommonsProcessing struct {
	CommonsProcessingStatistics
}

func (e *CommonsProcessing) GetStats() *ProcessingStatistics {
	return ptr(e.CommonsProcessingStatistics.Convert())
}
