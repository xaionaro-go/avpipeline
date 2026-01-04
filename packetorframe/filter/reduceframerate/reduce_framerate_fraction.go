// reduce_framerate_fraction.go implements a filter that reduces the frame rate by a fraction.

// Package reduceframerate provides a filter that reduces the frame rate.
package reduceframerate

import (
	"context"
	"fmt"
	"math"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

const (
	reduceFramerateDebug = false
)

type Filter struct {
	Mutex             xsync.Mutex
	FPSFractionGetter mathcondition.Getter[globaltypes.Rational]
	FrameCount        xsync.Map[int, uint64]
	LastSentPTS       xsync.Map[int, int64]
}

var _ condition.Condition = (*Filter)(nil)

func New(
	fpsFractionGetter mathcondition.Getter[globaltypes.Rational],
) *Filter {
	return &Filter{
		FPSFractionGetter: fpsFractionGetter,
	}
}

func (f *Filter) String() string {
	return fmt.Sprintf("ReduceFramerateFraction(%s)", f.FPSFractionGetter)
}

func (f *Filter) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) (_ret bool) {
	return xsync.DoA2R1(ctx, &f.Mutex, f.match, ctx, in)
}

func (f *Filter) match(
	ctx context.Context,
	in packetorframe.InputUnion,
) (_ret bool) {
	fraction := f.FPSFractionGetter.Get(ctx)
	assert(ctx, fraction.Num >= 0, "ReduceFramerate: fraction.Num must be >= 0", fraction)
	assert(ctx, fraction.Den >= 1, "ReduceFramerate: fraction.Den must be >= 1", fraction)
	assert(ctx, fraction.Den >= fraction.Num, "ReduceFramerate: fraction.Den must be >= fraction.Num", fraction)
	num, den := uint64(fraction.Num), uint64(fraction.Den)
	streamIdx := in.GetStreamIndex()
	if reduceFramerateDebug {
		logger.Tracef(ctx, "%s frame-or-packet from stream %d: pts:%d, dur:%d: %v/%v", in.GetMediaType(), streamIdx, in.GetPTS(), in.GetDuration(), num, den)
		defer func() {
			logger.Tracef(ctx, "/%s frame-or-packet from stream %d: pts:%d, dur:%d: %v/%v: %v", in.GetMediaType(), streamIdx, in.GetPTS(), in.GetDuration(), num, den, _ret)
		}()
	}

	// deciding whether to send the frame

	if num == 0 {
		return false // zero means no frames should pass
	}
	frameID, _ := f.FrameCount.Load(streamIdx)
	f.FrameCount.Store(streamIdx, frameID+1)
	eachN := float64(den) / float64(num)
	nDeviation := math.Remainder(float64(frameID%den), eachN)
	shouldPass := nDeviation >= 0 && nDeviation < 1.0
	if reduceFramerateDebug {
		logger.Tracef(ctx, "shouldPass: %t: frameID=%d, num=%d, den=%d, eachN=%f, nDeviation=%f", shouldPass, frameID, num, den, eachN, nDeviation)
	}
	if !shouldPass {
		return false
	}

	// adjusting the duration before sending the frame

	if in.GetMediaType() != astiav.MediaTypeVideo {
		return true // only video frames need duration adjustment
	}
	lastSentPTS, ok := f.LastSentPTS.Load(streamIdx)
	f.LastSentPTS.Store(streamIdx, in.GetPTS())
	if !ok {
		return true // first frame, no reason to skip it yet.
	}
	if lastSentPTS == astiav.NoPtsValue {
		return true // last PTS is unknown, cannot adjust duration.
	}
	duration := in.GetPTS() - lastSentPTS
	dur := in.GetDuration()
	expectedDuration := float64(dur) * float64(den) / float64(num)
	if duration > int64(expectedDuration)*2 {
		duration = int64(expectedDuration)
	}
	in.SetDuration(duration)
	if reduceFramerateDebug {
		logger.Tracef(ctx, "dur: %d", dur)
	}
	return true
}
