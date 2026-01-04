// filter.go implements a filter that ensures monotonic PTS.

// Package monotonicpts provides a filter that ensures monotonic PTS.
package monotonicpts

import (
	"context"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/xsync"
)

type StreamKey struct {
	Source packet.Source
}

type Filter struct {
	xsync.Mutex
	LatestPTS      time.Duration
	SourcePTSShift map[StreamKey]int64
	ShouldCorrect  bool
}

func New(
	shouldCorrect bool,
) *Filter {
	return &Filter{
		ShouldCorrect:  shouldCorrect,
		SourcePTSShift: make(map[StreamKey]int64),
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
	dtsIntOrg := in.GetDTS()
	ptsIntOrig := in.GetPTS()
	if ptsIntOrig < dtsIntOrg && dtsIntOrg != astiav.NoPtsValue {
		logger.Tracef(ctx, "MonotonicPTS filter: frame PTS %d < DTS %d, skipping", ptsIntOrig, in.GetDTS())
		return false
	}
	pts := avconv.Duration(in.GetPTS(), in.GetTimeBase())
	var packetSource packet.Source
	if in.Packet != nil {
		packetSource = in.Packet.GetSource()
	}
	streamKey := StreamKey{
		Source: packetSource,
	}
	var shift time.Duration
	ptsInt := ptsIntOrig
	if shiftInt := f.SourcePTSShift[streamKey]; shiftInt != 0 {
		ptsInt += shiftInt
		in.SetPTS(ptsInt)
		if in.GetDTS() < ptsInt {
			in.SetDTS(ptsInt)
		}
		shift = avconv.Duration(shiftInt, in.GetTimeBase())
	}
	if in.GetStreamIndex() != 0 {
		logger.Tracef(ctx, "MonotonicPTS filter: non-first stream (index: %d), skipping the calculations", in.GetStreamIndex())
		return true
	}
	logger.Tracef(ctx, "MonotonicPTS filter: curPTS=%v+%v (int: %v+?), latestPTS=%v", pts, shift, ptsInt, f.LatestPTS)
	pts += shift
	if pts+100*time.Millisecond > f.LatestPTS {
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
	f.SourcePTSShift[streamKey] = shiftInt
	logger.Debugf(ctx,
		"MonotonicPTS filter: new TS shift %v for stream %+v (%s)",
		avconv.Duration(shiftInt, in.GetTimeBase()), streamKey, in.GetMediaType(),
	)
	if in.GetDTS() < nextPTSInt {
		in.SetDTS(nextPTSInt)
	}
	nextPTS := avconv.Duration(nextPTSInt, in.GetTimeBase())
	f.LatestPTS = nextPTS
	return true
}
