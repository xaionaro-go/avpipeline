package condition

import (
	"context"
	"fmt"
	"math"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

const (
	reduceFramerateDebug = false
)

type ReduceFramerateFractionT struct {
	FPSFractionGetter mathcondition.Getter[globaltypes.Rational]
	FrameCount        xsync.Map[int, uint64]
	LastSentPTS       xsync.Map[int, int64]
}

var _ Condition = (*ReduceFramerateFractionT)(nil)

func ReduceFramerateFraction(
	fpsFractionGetter mathcondition.Getter[globaltypes.Rational],
) *ReduceFramerateFractionT {
	return &ReduceFramerateFractionT{
		FPSFractionGetter: fpsFractionGetter,
	}
}

func (f *ReduceFramerateFractionT) String() string {
	return fmt.Sprintf("ReduceFramerateFraction(%s)", f.FPSFractionGetter)
}

func (f *ReduceFramerateFractionT) Match(
	ctx context.Context,
	i frame.Input,
) (_ret bool) {
	fraction := f.FPSFractionGetter.Get(ctx)
	assert(ctx, fraction.Num >= 0, "ReduceFramerate: fraction.Num must be >= 0", fraction)
	assert(ctx, fraction.Den >= 1, "ReduceFramerate: fraction.Den must be >= 1", fraction)
	assert(ctx, fraction.Den >= fraction.Num, "ReduceFramerate: fraction.Den must be >= fraction.Num", fraction)
	num, den := uint64(fraction.Num), uint64(fraction.Den)
	if reduceFramerateDebug {
		logger.Tracef(ctx, "frame %p from stream %d: pts:%d, dur:%d: %v/%v", i.Frame, i.StreamIndex, i.Frame.Pts(), i.Frame.Duration(), num, den)
		defer func() {
			logger.Tracef(ctx, "/frame %p from stream %d: pts:%d, dur:%d: %v/%v: %v", i.Frame, i.StreamIndex, i.Frame.Pts(), i.Frame.Duration(), num, den, _ret)
		}()
	}
	streamIdx := i.StreamIndex

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

	if i.GetMediaType() != astiav.MediaTypeVideo {
		return true // only video frames need duration adjustment
	}
	lastSentPTS, ok := f.LastSentPTS.Load(i.StreamIndex)
	f.LastSentPTS.Store(i.StreamIndex, i.Frame.Pts())
	if !ok {
		return true // first frame, no reason to skip it yet.
	}
	if lastSentPTS == astiav.NoPtsValue {
		return true // last PTS is unknown, cannot adjust duration.
	}
	duration := i.Frame.Pts() - lastSentPTS
	expectedDuration := float64(i.Frame.Duration()) * float64(den) / float64(num)
	if duration > int64(expectedDuration)*2 {
		duration = int64(expectedDuration)
	}
	i.Frame.SetDuration(duration)
	if reduceFramerateDebug {
		logger.Tracef(ctx, "dur: %d", i.Frame.Duration())
	}
	return true
}
