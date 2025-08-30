package types

import (
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type Counters struct {
	Packets CountersSection
	Frames  CountersSection
}

func NewCounters() *Counters {
	return &Counters{
		Packets: NewCountersSection(),
		Frames:  NewCountersSection(),
	}
}

type CountersSection struct {
	Missed   types.CountersSubSection
	Received types.CountersSubSection
	Sent     types.CountersSubSection
}

func NewCountersSection() CountersSection {
	return CountersSection{
		Missed:   types.NewCountersSubSection(),
		Received: types.NewCountersSubSection(),
		Sent:     types.NewCountersSubSection(),
	}
}

func (s *CountersSection) ToStats() types.StatisticsSection {
	return types.StatisticsSection{
		Missed:   s.Missed.ToStats(),
		Received: s.Received.ToStats(),
		Sent:     s.Sent.ToStats(),
	}
}

func (stats *Counters) ToStats() types.Statistics {
	return types.Statistics{
		Packets: stats.Packets.ToStats(),
		Frames:  stats.Frames.ToStats(),
	}
}

func FromStatisticsSection(s types.StatisticsSection) *CountersSection {
	return &CountersSection{
		Missed:   *s.Missed.ToCounters(),
		Received: *s.Received.ToCounters(),
		Sent:     *s.Sent.ToCounters(),
	}
}

func FromProcessingStatistics(s *types.Statistics) *Counters {
	return &Counters{
		Packets: *FromStatisticsSection(s.Packets),
		Frames:  *FromStatisticsSection(s.Frames),
	}
}

func ToStatistics(nc *Counters, pc *processortypes.Counters) types.Statistics {
	if nc == nil || pc == nil {
		return types.Statistics{}
	}
	return types.Statistics{
		Packets: types.StatisticsSection{
			Missed:    nc.Packets.Missed.ToStats(),
			Received:  nc.Packets.Received.ToStats(),
			Processed: pc.Packets.Processed.ToStats(),
			Generated: pc.Packets.Generated.ToStats(),
			Sent:      nc.Packets.Sent.ToStats(),
		},
		Frames: types.StatisticsSection{
			Missed:    nc.Frames.Missed.ToStats(),
			Received:  nc.Frames.Received.ToStats(),
			Processed: pc.Frames.Processed.ToStats(),
			Generated: pc.Frames.Generated.ToStats(),
			Sent:      nc.Frames.Sent.ToStats(),
		},
	}
}
