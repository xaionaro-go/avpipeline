package monotonicpts

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/xsync"
)

type Filter struct {
	xsync.Mutex
	LatestPTS      time.Duration
	SourcePTSShift map[packet.Source]int64
	ShouldCorrect  bool
}

func New(
	shouldCorrect bool,
) *Filter {
	return &Filter{
		ShouldCorrect:  shouldCorrect,
		SourcePTSShift: make(map[packet.Source]int64),
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
	ptsIntOrig := in.GetPTS()
	pts := avconv.Duration(in.GetPTS(), in.GetTimeBase())
	var packetSource packet.Source
	if in.Packet != nil {
		packetSource = in.Packet.GetSource()
	}
	var shift time.Duration
	ptsInt := ptsIntOrig
	if shiftInt := f.SourcePTSShift[packetSource]; shiftInt != 0 {
		ptsInt += shiftInt
		in.SetPTS(ptsInt)
		if in.GetDTS() < ptsInt {
			in.SetDTS(ptsInt)
		}
		shift = avconv.Duration(shiftInt, in.GetTimeBase())
	}
	logger.Tracef(ctx, "MonotonicPTS filter: current PTS=%v+%v, latest PTS=%v", pts, shift, f.LatestPTS)
	pts += shift
	if pts > f.LatestPTS {
		f.LatestPTS = pts
		return true
	}
	if !f.ShouldCorrect {
		return false
	}
	latestPTSInt := avconv.FromDuration(f.LatestPTS, in.GetTimeBase())
	nextPTSInt := latestPTSInt + 1
	in.SetPTS(nextPTSInt)
	shiftInt := nextPTSInt - ptsIntOrig
	f.SourcePTSShift[packetSource] = shiftInt
	logger.Debugf(ctx,
		"MonotonicPTS filter: new TS shift %v for source %+v",
		avconv.Duration(shiftInt, in.GetTimeBase()), packetSource,
	)
	if in.GetDTS() < nextPTSInt {
		in.SetDTS(nextPTSInt)
	}
	nextPTS := avconv.Duration(nextPTSInt, in.GetTimeBase())
	f.LatestPTS = nextPTS
	return true
}
