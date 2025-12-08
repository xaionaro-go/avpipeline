package monotonicpts

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/xsync"
)

type Filter struct {
	xsync.Mutex
	LatestPTS     time.Duration
	ShouldCorrect bool
}

func New(
	shouldCorrect bool,
) *Filter {
	return &Filter{
		ShouldCorrect: shouldCorrect,
	}
}

var _ condition.Condition = (*Filter)(nil)

func (f *Filter) String() string {
	return fmt.Sprintf("MonotonicPTS(%v)", f.LatestPTS)
}

func (f *Filter) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	return xsync.DoA2R1(ctx, &f.Mutex, f.match, ctx, in)
}

func (f *Filter) match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	pts := avconv.Duration(in.GetPTS(), in.GetTimeBase())
	if pts > f.LatestPTS {
		f.LatestPTS = pts
		return true
	}
	if !f.ShouldCorrect {
		return false
	}
	nextPTSInt := in.GetPTS() + 1
	in.SetPTS(nextPTSInt)
	if in.GetDTS() < nextPTSInt {
		in.SetDTS(nextPTSInt)
	}
	nextPTS := avconv.Duration(nextPTSInt, in.GetTimeBase())
	f.LatestPTS = nextPTS
	return true
}
