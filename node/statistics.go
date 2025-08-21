package node

import (
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/types"
)

type ProcessingFramesOrPacketsStatisticsSection = types.ProcessingFramesOrPacketsStatisticsSection
type ProcessingFramesOrPacketsStatistics = types.ProcessingFramesOrPacketsStatistics
type ProcessingStatistics = types.ProcessingStatistics

type FramesOrPacketsStatisticsSection struct {
	Unknown atomic.Uint64
	Other   atomic.Uint64
	Video   atomic.Uint64
	Audio   atomic.Uint64
}

func (s *FramesOrPacketsStatisticsSection) Convert() ProcessingFramesOrPacketsStatisticsSection {
	return ProcessingFramesOrPacketsStatisticsSection{
		Unknown: s.Unknown.Load(),
		Other:   s.Other.Load(),
		Video:   s.Video.Load(),
		Audio:   s.Audio.Load(),
	}
}

type FramesOrPacketsStatistics struct {
	Read   FramesOrPacketsStatisticsSection
	Missed FramesOrPacketsStatisticsSection
	Wrote  FramesOrPacketsStatisticsSection
}

func (s *FramesOrPacketsStatistics) Convert() ProcessingFramesOrPacketsStatistics {
	return ProcessingFramesOrPacketsStatistics{
		Read:   s.Read.Convert(),
		Missed: s.Missed.Convert(),
		Wrote:  s.Wrote.Convert(),
	}
}

func (stats *Statistics) Convert() ProcessingStatistics {
	return ProcessingStatistics{
		BytesCountRead:  stats.BytesCountRead.Load(),
		BytesCountWrote: stats.BytesCountWrote.Load(),
		Packets:         stats.Packets.Convert(),
		Frames:          stats.Frames.Convert(),
	}
}

type Statistics struct {
	BytesCountRead   atomic.Uint64
	BytesCountMissed atomic.Uint64
	BytesCountWrote  atomic.Uint64
	Packets          FramesOrPacketsStatistics
	Frames           FramesOrPacketsStatistics
}

func fromProcessingFramesOrPacketsStatsSection(
	s ProcessingFramesOrPacketsStatisticsSection,
) *FramesOrPacketsStatisticsSection {
	stats := &FramesOrPacketsStatisticsSection{}
	stats.Audio.Store(s.Audio)
	stats.Video.Store(s.Video)
	stats.Other.Store(s.Other)
	stats.Unknown.Store(s.Unknown)
	return stats
}

func fromProcessingFramesOrPacketsStats(
	s ProcessingFramesOrPacketsStatistics,
) *FramesOrPacketsStatistics {
	stats := &FramesOrPacketsStatistics{}
	stats.Read = *fromProcessingFramesOrPacketsStatsSection(s.Read)
	stats.Wrote = *fromProcessingFramesOrPacketsStatsSection(s.Wrote)
	stats.Missed = *fromProcessingFramesOrPacketsStatsSection(s.Missed)
	return stats
}

func FromProcessingStatistics(s *ProcessingStatistics) *Statistics {
	stats := &Statistics{}
	stats.BytesCountRead.Store(s.BytesCountRead)
	stats.BytesCountWrote.Store(s.BytesCountWrote)
	stats.Packets = *fromProcessingFramesOrPacketsStats(s.Packets)
	stats.Frames = *fromProcessingFramesOrPacketsStats(s.Frames)
	return stats
}

func (e *Statistics) GetStats() *ProcessingStatistics {
	return ptr(e.Convert())
}
