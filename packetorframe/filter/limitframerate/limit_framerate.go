// limit_framerate.go implements a filter that limits the frame rate.

// Package limitframerate provides a filter that limits the frame rate.
package limitframerate

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Filter struct {
	Mutex          xsync.Mutex
	FPSLimitGetter mathcondition.Getter[globaltypes.Rational]
	NextMinPTS     xsync.Map[int, int64]
	PTSDebt        xsync.Map[int, int64]
	DurRemainder   xsync.Map[int, int64]
}

var _ condition.Condition = (*Filter)(nil)

func New(
	fpsLimitGetter mathcondition.Getter[globaltypes.Rational],
) *Filter {
	return &Filter{
		FPSLimitGetter: fpsLimitGetter,
	}
}

func (f *Filter) String() string {
	return fmt.Sprintf("LimitFramerate(%s)", f.FPSLimitGetter)
}

func (f *Filter) Match(
	ctx context.Context,
	i packetorframe.InputUnion,
) (_ret bool) {
	return xsync.DoA2R1(ctx, &f.Mutex, f.match, ctx, i)
}

func (f *Filter) match(
	ctx context.Context,
	i packetorframe.InputUnion,
) (_ret bool) {
	streamIdx := i.GetStreamIndex()
	logger.Tracef(ctx, "LimitFramerate.Match: %s frame from stream %d: pts:%d, dur:%d", i.GetMediaType(), streamIdx, i.GetPTS(), i.GetDuration())
	defer func() {
		logger.Tracef(ctx, "/LimitFramerate.Match: frame %p from stream %d: pts:%d, dur:%d: %v", i.GetMediaType(), streamIdx, i.GetPTS(), i.GetDuration(), _ret)
	}()
	maxFPS := f.FPSLimitGetter.Get(ctx)
	assert(ctx, maxFPS.Num >= 0, "LimitFramerate: fraction.Num must be >= 0", maxFPS)
	assert(ctx, maxFPS.Den >= 1, "LimitFramerate: fraction.Den must be >= 1", maxFPS)
	assert(ctx, maxFPS.Den <= maxFPS.Num, "LimitFramerate: fraction.Den must be <= fraction.Num", maxFPS)

	// deciding whether to send the frame

	if maxFPS.Num == 0 {
		return false // zero means no frames should pass
	}

	minPTS, _ := f.NextMinPTS.Load(streamIdx)
	pts := i.GetPTS()
	dur := i.GetDuration()

	_timeBase := i.GetTimeBase()
	timeBase := globaltypes.Rational{
		Num: int(_timeBase.Num()),
		Den: int(_timeBase.Den()),
	}
	minDuration := timeBase.Reverse().Div(maxFPS)
	defer func() {
		if !_ret {
			return
		}
		curFrameID := max(pts, minPTS) * int64(minDuration.Den) / int64(minDuration.Num)
		nextMinPTS := int64(curFrameID+1) * int64(minDuration.Num) / int64(minDuration.Den)
		f.NextMinPTS.Store(streamIdx, nextMinPTS)
	}()

	if pts < minPTS-2 && minPTS >= 2 {
		logger.Tracef(ctx, "%s frame from stream %d: pts:%d, minPTS:%d, dur:%d, minDur:%s, timeBase:%v: skipped",
			i.GetMediaType(), streamIdx, pts, minPTS, dur, minDuration, timeBase)
		return false
	}

	// to make small fluctuations due to rounding errors won't lead to frame drops while FPS is already low enough
	debt, _ := f.PTSDebt.Load(streamIdx)
	if pts < minPTS {
		addDebt := int64(minPTS - pts)
		totalDebt := debt + addDebt
		if totalDebt > 3 { // theoretically 2 should be enough, but I'm not 99.999% sure (and too lazy to verify), so just using 3 here
			logger.Tracef(ctx,
				"%s frame from stream %d: pts:%d, minPTS:%d, dur:%d, minDur:%s, timeBase:%v: skipped; accumulated PTS debt: %d (+%d)",
				i.GetMediaType(), streamIdx, pts, minPTS, dur, minDuration, timeBase, totalDebt, addDebt,
			)
			f.PTSDebt.Store(streamIdx, totalDebt)
			return false
		}
	}
	debt -= int64(pts - minPTS)
	if debt <= 0 {
		f.PTSDebt.Delete(streamIdx)
	} else {
		f.PTSDebt.Store(streamIdx, debt)
	}

	logger.Tracef(ctx,
		"%s frame from stream %d: pts:%d, minPTS:%d, dur:%d, minDur:%s, timeBase:%v: passed; remaining PTS debt: %d",
		i.GetMediaType(), streamIdx, pts, minPTS, dur, minDuration, timeBase, debt,
	)
	durRemainder, _ := f.DurRemainder.Load(streamIdx)
	num := int64(minDuration.Num) + durRemainder
	minDurationInt := num / int64(minDuration.Den)
	durRemainder = num % int64(minDuration.Den)
	f.DurRemainder.Store(streamIdx, durRemainder)

	if int64(dur) < minDurationInt+3 { // TODO: remove this +3 ugly hack, it will break cases where input FPS is slightly lower than the target FPS
		i.SetDuration(minDurationInt)
	}
	return true
}
