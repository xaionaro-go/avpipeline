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
	Packets StatisticsSubSection
	Frames  StatisticsSubSection
}

type Statistics struct {
	Addressed StatisticsSection
	Missed    StatisticsSection
	Received  StatisticsSection
	Processed StatisticsSection
	Generated StatisticsSection
	Sent      StatisticsSection
}

type CountersItem struct {
	Count atomic.Uint64
	Bytes atomic.Uint64
}

func NewCountersItem() *CountersItem {
	return &CountersItem{}
}

func (c *CountersItem) Increment(msgSize uint64) *CountersItem {
	c.Count.Add(1)
	c.Bytes.Add(msgSize)
	return c
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

func (s *CountersSubSection) Get(mediaType MediaType) *CountersItem {
	switch mediaType {
	case MediaTypeVideo:
		return s.Video
	case MediaTypeAudio:
		return s.Audio
	default:
		return s.Other
	}
}

func (s *CountersSubSection) Increment(mediaType MediaType, msgSize uint64) *CountersItem {
	return s.Get(mediaType).Increment(msgSize)
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

type CountersSection struct {
	Packets CountersSubSection
	Frames  CountersSubSection
}

func NewCountersSection() CountersSection {
	return CountersSection{
		Packets: NewCountersSubSection(),
		Frames:  NewCountersSubSection(),
	}
}

func (s *CountersSection) ToStats() StatisticsSection {
	return StatisticsSection{
		Packets: s.Packets.ToStats(),
		Frames:  s.Frames.ToStats(),
	}
}

func (s StatisticsSection) ToCounters() *CountersSection {
	stats := &CountersSection{}
	stats.Packets = *s.Packets.ToCounters()
	stats.Frames = *s.Frames.ToCounters()
	return stats
}

func (s *CountersSection) TotalCount() uint64 {
	var total uint64
	total += s.Packets.TotalCount()
	total += s.Frames.TotalCount()
	return total
}

func (s *CountersSection) TotalBytes() uint64 {
	var total uint64
	total += s.Packets.TotalBytes()
	total += s.Frames.TotalBytes()
	return total
}

type CountersSubSectionID int

const (
	UndefinedSubSectionID CountersSubSectionID = CountersSubSectionID(iota)
	CountersSubSectionIDPackets
	CountersSubSectionIDFrames
	EndOfCountersSubSectionID
)

func (s *CountersSection) Increment(
	subSection CountersSubSectionID,
	mediaType MediaType,
	msgSize uint64,
) *CountersItem {
	return s.Get(subSection).Get(mediaType).Increment(msgSize)
}

func (s *CountersSection) Get(id CountersSubSectionID) *CountersSubSection {
	switch id {
	case CountersSubSectionIDPackets:
		return &s.Packets
	case CountersSubSectionIDFrames:
		return &s.Frames
	default:
		return nil
	}
}
