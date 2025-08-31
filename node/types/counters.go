package types

import (
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Counters struct {
	Missed   globaltypes.CountersSection
	Received globaltypes.CountersSection
	Sent     globaltypes.CountersSection
}

func NewCounters() *Counters {
	return &Counters{
		Missed:   globaltypes.NewCountersSection(),
		Received: globaltypes.NewCountersSection(),
		Sent:     globaltypes.NewCountersSection(),
	}
}

func ToStatistics(nc *Counters, pc *processortypes.Counters) globaltypes.Statistics {
	if nc == nil || pc == nil {
		return globaltypes.Statistics{}
	}
	return globaltypes.Statistics{
		Received:  nc.Received.ToStats(),
		Processed: pc.Processed.ToStats(),
		Missed:    nc.Missed.ToStats(),
		Generated: pc.Generated.ToStats(),
		Sent:      nc.Sent.ToStats(),
	}
}

type SectionID int

const (
	UndefinedSectionID = SectionID(iota)
	SectionIDMissed
	SectionIDReceived
	SectionIDSent
	EndOfSectionID
)

func (counters *Counters) GetSectionByID(id SectionID) *globaltypes.CountersSection {
	switch id {
	case SectionIDMissed:
		return &counters.Missed
	case SectionIDReceived:
		return &counters.Received
	case SectionIDSent:
		return &counters.Sent
	default:
		return nil
	}
}
