package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/types"
)

type CountersSectionID int

const (
	CountersSectionIDPackets CountersSectionID = iota
	CountersSectionIDFrames
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

func (c *Counters) Increment(
	ctx context.Context,
	section CountersSectionID,
	mediaType types.MediaType,
	msgSize uint64,
) {
	switch section {
	case CountersSectionIDPackets:
		incrementCountersInSection(&c.Packets.Processed, mediaType, msgSize)
	case CountersSectionIDFrames:
		incrementCountersInSection(&c.Frames.Processed, mediaType, msgSize)
	default:
		logger.Errorf(ctx, "unknown counters section: %d", section)
	}
}

func incrementCountersInSection(
	s *types.CountersSubSection,
	mediaType types.MediaType,
	msgSize uint64,
) {
	switch mediaType {
	case types.MediaTypeVideo:
		incrementInSubsection(s.Video, msgSize)
	case types.MediaTypeAudio:
		incrementInSubsection(s.Audio, msgSize)
	default:
		incrementInSubsection(s.Other, msgSize)
	}
}

func incrementInSubsection(
	s *types.CountersItem,
	msgSize uint64,
) {
	s.Count.Add(1)
	s.Bytes.Add(msgSize)
}

type CountersSection struct {
	Processed types.CountersSubSection
	Generated types.CountersSubSection
}

func NewCountersSection() CountersSection {
	return CountersSection{
		Processed: types.NewCountersSubSection(),
		Generated: types.NewCountersSubSection(),
	}
}
