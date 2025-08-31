package types

import (
	"context"

	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type CountersSectionID int

const (
	CountersSectionIDProcessed CountersSectionID = iota
	CountersSectionIDGenerated
)

type Counters struct {
	Processed globaltypes.CountersSection
	Generated globaltypes.CountersSection
}

func NewCounters() *Counters {
	return &Counters{
		Processed: globaltypes.NewCountersSection(),
		Generated: globaltypes.NewCountersSection(),
	}
}

func (c *Counters) Increment(
	ctx context.Context,
	section CountersSectionID,
	subsection globaltypes.CountersSubSectionID,
	mediaType globaltypes.MediaType,
	msgSize uint64,
) *globaltypes.CountersItem {
	switch section {
	case CountersSectionIDProcessed:
		return c.Processed.Increment(subsection, mediaType, msgSize)
	case CountersSectionIDGenerated:
		return c.Generated.Increment(subsection, mediaType, msgSize)
	default:
		return nil
	}
}
