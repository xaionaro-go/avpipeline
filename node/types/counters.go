package types

import (
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Counters struct {
	Addressed globaltypes.CountersSection
	Received  globaltypes.CountersSection
	Missed    globaltypes.CountersSection
	Sent      globaltypes.CountersSection
}

func NewCounters() *Counters {
	return &Counters{
		Addressed: globaltypes.NewCountersSection(),
		Received:  globaltypes.NewCountersSection(),
		Missed:    globaltypes.NewCountersSection(),
		Sent:      globaltypes.NewCountersSection(),
	}
}

func ToStatistics(nc *Counters, pc *processortypes.Counters) globaltypes.Statistics {
	if nc == nil || pc == nil {
		return globaltypes.Statistics{}
	}
	return globaltypes.Statistics{
		Addressed: nc.Addressed.ToStats(),
		Received:  nc.Received.ToStats(),
		Missed:    nc.Missed.ToStats(),
		Processed: pc.Processed.ToStats(),
		Generated: pc.Generated.ToStats(),
		Omitted:   pc.Omitted.ToStats(),
		Sent:      nc.Sent.ToStats(),
	}
}

type SectionID int

const (
	UndefinedSectionID = SectionID(iota)
	SectionIDAddressed
	SectionIDMissed
	SectionIDReceived
	SectionIDSent
	EndOfSectionID
)

func (counters *Counters) Get(id SectionID) *globaltypes.CountersSection {
	switch id {
	case SectionIDAddressed:
		return &counters.Addressed
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
