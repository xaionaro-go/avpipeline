package avpipeline

import (
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/types"
)

type RecoderFramesStatistics = types.EncoderFramesStatistics
type RecoderStatistics = types.EncoderStatistics

type CommonsRecoderFramesStatistics struct {
	Unparsed         atomic.Uint64
	VideoUnprocessed atomic.Uint64
	AudioUnprocessed atomic.Uint64
	VideoProcessed   atomic.Uint64
	AudioProcessed   atomic.Uint64
}

type CommonsRecoderStatistics struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
	FramesRead      CommonsRecoderFramesStatistics
	FramesWrote     CommonsRecoderFramesStatistics
}

func (stats *CommonsRecoderStatistics) Convert() RecoderStatistics {
	return RecoderStatistics{
		BytesCountRead:  stats.BytesCountRead.Load(),
		BytesCountWrote: stats.BytesCountWrote.Load(),
		FramesRead: RecoderFramesStatistics{
			Unparsed:         stats.FramesRead.Unparsed.Load(),
			VideoUnprocessed: stats.FramesRead.VideoUnprocessed.Load(),
			AudioUnprocessed: stats.FramesRead.AudioUnprocessed.Load(),
			VideoProcessed:   stats.FramesRead.VideoProcessed.Load(),
			AudioProcessed:   stats.FramesRead.AudioProcessed.Load(),
		},
		FramesWrote: RecoderFramesStatistics{
			Unparsed:         stats.FramesWrote.Unparsed.Load(),
			VideoUnprocessed: stats.FramesWrote.VideoUnprocessed.Load(),
			AudioUnprocessed: stats.FramesWrote.AudioUnprocessed.Load(),
			VideoProcessed:   stats.FramesWrote.VideoProcessed.Load(),
			AudioProcessed:   stats.FramesWrote.AudioProcessed.Load(),
		},
	}
}

type CommonsRecoder struct {
	CommonsRecoderStatistics
}

func (e *CommonsRecoder) GetStats() *RecoderStatistics {
	return ptr(e.CommonsRecoderStatistics.Convert())
}
