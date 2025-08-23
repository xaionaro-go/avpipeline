package condition

import (
	"context"
	"fmt"
	"sync/atomic"
)

type SampledCond struct {
	EachN     uint64
	CurID     atomic.Uint64
	Condition Condition
}

var _ Condition = (*SampledCond)(nil)

func Sampled(
	eachN uint64,
	condition Condition,
) *SampledCond {
	return &SampledCond{
		EachN:     eachN,
		Condition: condition,
	}
}

func (f *SampledCond) String() string {
	return fmt.Sprintf("Sampled(1/%d: %s)", f.EachN, f.Condition)
}

func (f *SampledCond) Match(
	ctx context.Context,
	in Input,
) bool {
	if f.EachN == 0 {
		return true
	}
	id := f.CurID.Add(1)
	if id%f.EachN != 0 {
		return true
	}
	return f.Condition.Match(ctx, in)
}
