package types

import (
	"sync/atomic"
)

type StatisticsItem struct {
	Count uint64 `json:",omitempty"`
	Bytes uint64 `json:",omitempty"`
}

func (c StatisticsItem) ToCounters() *CountersItem {
	result := CountersItem{}
	result.Count.Store(c.Count)
	result.Bytes.Store(c.Bytes)
	return &result
}

type StatisticsSubSection struct {
	Unknown StatisticsItem `json:",omitempty"`
	Other   StatisticsItem `json:",omitempty"`
	Video   StatisticsItem `json:",omitempty"`
	Audio   StatisticsItem `json:",omitempty"`
}

type StatisticsSection struct {
	Missed    StatisticsSubSection
	Received  StatisticsSubSection
	Processed StatisticsSubSection
	Generated StatisticsSubSection
	Sent      StatisticsSubSection
}

type Statistics struct {
	Packets StatisticsSection
	Frames  StatisticsSection
}

type CountersItem struct {
	Count atomic.Uint64
	Bytes atomic.Uint64
}

func NewCountersItem() *CountersItem {
	return &CountersItem{}
}

func (c *CountersItem) Increment(msgSize uint64) {
	c.Count.Add(1)
	c.Bytes.Add(msgSize)
}

func (c *CountersItem) ToStats() StatisticsItem {
	return StatisticsItem{
		Count: c.Count.Load(),
		Bytes: c.Bytes.Load(),
	}
}

type CountersSubSection struct {
	Video   *CountersItem
	Audio   *CountersItem
	Other   *CountersItem
	Unknown *CountersItem
}

func NewCountersSubSection() CountersSubSection {
	return CountersSubSection{
		Video:   NewCountersItem(),
		Audio:   NewCountersItem(),
		Other:   NewCountersItem(),
		Unknown: NewCountersItem(),
	}
}

func (s *CountersSubSection) Increment(mediaType MediaType, msgSize uint64) {
	switch mediaType {
	case MediaTypeVideo:
		s.Video.Increment(msgSize)
	case MediaTypeAudio:
		s.Audio.Increment(msgSize)
	default:
		s.Other.Increment(msgSize)
	}
}

func (s *CountersSubSection) TotalCount() uint64 {
	var total uint64
	total += s.Video.Count.Load()
	total += s.Audio.Count.Load()
	total += s.Other.Count.Load()
	total += s.Unknown.Count.Load()
	return total
}

func (s *CountersSubSection) TotalBytes() uint64 {
	var total uint64
	total += s.Video.Bytes.Load()
	total += s.Audio.Bytes.Load()
	total += s.Other.Bytes.Load()
	total += s.Unknown.Bytes.Load()
	return total
}

func (s *CountersSubSection) ToStats() StatisticsSubSection {
	return StatisticsSubSection{
		Unknown: s.Unknown.ToStats(),
		Other:   s.Other.ToStats(),
		Video:   s.Video.ToStats(),
		Audio:   s.Audio.ToStats(),
	}
}

func (s StatisticsSubSection) ToCounters() *CountersSubSection {
	stats := &CountersSubSection{}
	stats.Audio = s.Audio.ToCounters()
	stats.Video = s.Video.ToCounters()
	stats.Other = s.Other.ToCounters()
	stats.Unknown = s.Unknown.ToCounters()
	return stats
}
