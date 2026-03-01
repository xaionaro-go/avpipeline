// reduce_framerate_fraction_test.go provides tests for reduce framerate fraction filter.

package reduceframerate

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

func TestFilter_String(t *testing.T) {
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 1, Den: 2},
	})
	testifyassert.Contains(t, f.String(), "ReduceFramerateFraction")
}

func TestReduceFramerate_ZeroNum(t *testing.T) {
	ctx := context.Background()
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 0, Den: 1},
	})

	fr := frame.Input{
		Frame: astiav.AllocFrame(),
		StreamInfo: &frame.StreamInfo{
			CodecParameters: astiav.AllocCodecParameters(),
		},
	}
	fr.StreamInfo.CodecParameters.SetMediaType(astiav.MediaTypeAudio)
	in := packetorframe.InputUnion{Frame: &fr}

	// num==0 → all frames dropped
	testifyassert.False(t, f.Match(ctx, in))
	testifyassert.False(t, f.Match(ctx, in))
}

func TestReduceFramerate_Video_DurationAdjustment(t *testing.T) {
	ctx := context.Background()
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 1, Den: 2},
	})

	fr := frame.Input{
		Frame: astiav.AllocFrame(),
		StreamInfo: &frame.StreamInfo{
			CodecParameters: astiav.AllocCodecParameters(),
		},
	}
	fr.StreamInfo.CodecParameters.SetMediaType(astiav.MediaTypeVideo)

	// 1/2 fraction: pass every other frame, eachN=2
	// Frame 0: remainder(0 % 2, 2) = 0 → pass (first frame, no duration adjustment)
	// Frame 1: remainder(1 % 2, 2) = 1 → drop
	// Frame 2: remainder(0 % 2, 2) = 0 → pass (duration adjusted)
	// Frame 3: remainder(1 % 2, 2) = 1 → drop
	// Frame 4: remainder(0 % 2, 2) = 0 → pass

	pts := int64(0)
	in := func() packetorframe.InputUnion {
		avFrame := astiav.AllocFrame()
		avFrame.SetPts(pts)
		avFrame.SetDuration(1000)
		input := frame.Input{
			Frame: avFrame,
			StreamInfo: &frame.StreamInfo{
				CodecParameters: fr.StreamInfo.CodecParameters,
			},
		}
		return packetorframe.InputUnion{Frame: &input}
	}

	// frame 0: pts=0, passes (first video frame)
	i0 := in()
	testifyassert.True(t, f.Match(ctx, i0))

	// frame 1: pts=1000, drops
	pts = 1000
	i1 := in()
	testifyassert.False(t, f.Match(ctx, i1))

	// frame 2: pts=2000, passes with duration adjustment
	pts = 2000
	i2 := in()
	testifyassert.True(t, f.Match(ctx, i2))
	// duration should be pts(2000) - lastSentPTS(0) = 2000
	testifyassert.Equal(t, int64(2000), i2.Frame.Frame.Duration())

	// frame 3: pts=3000, drops
	pts = 3000
	i3 := in()
	testifyassert.False(t, f.Match(ctx, i3))

	// frame 4: pts=4000, passes
	pts = 4000
	i4 := in()
	testifyassert.True(t, f.Match(ctx, i4))
	testifyassert.Equal(t, int64(2000), i4.Frame.Frame.Duration())
}

func TestReduceFramerate_MultipleStreams(t *testing.T) {
	ctx := context.Background()
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 1, Den: 2},
	})

	makeFrame := func(streamIdx int) packetorframe.InputUnion {
		cp := astiav.AllocCodecParameters()
		cp.SetMediaType(astiav.MediaTypeAudio)
		fr := frame.Input{
			Frame: astiav.AllocFrame(),
			StreamInfo: &frame.StreamInfo{
				CodecParameters: cp,
				StreamIndex:     streamIdx,
			},
		}
		return packetorframe.InputUnion{Frame: &fr}
	}

	// Each stream maintains its own counter
	in0 := makeFrame(0)
	in1 := makeFrame(1)

	// Stream 0: frame 0 → pass, frame 1 → drop
	testifyassert.True(t, f.Match(ctx, in0))
	testifyassert.False(t, f.Match(ctx, in0))
	// Stream 1: frame 0 → pass (independent counter)
	testifyassert.True(t, f.Match(ctx, in1))
	testifyassert.False(t, f.Match(ctx, in1))
}

func TestReduceFramerate_Video_NoPtsValue(t *testing.T) {
	ctx := context.Background()
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 1, Den: 2},
	})

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)

	makeVideoFrame := func(pts int64) packetorframe.InputUnion {
		avFrame := astiav.AllocFrame()
		avFrame.SetPts(pts)
		avFrame.SetDuration(1000)
		input := frame.Input{
			Frame: avFrame,
			StreamInfo: &frame.StreamInfo{
				CodecParameters: cp,
			},
		}
		return packetorframe.InputUnion{Frame: &input}
	}

	// Frame 0: passes, first frame
	i0 := makeVideoFrame(0)
	testifyassert.True(t, f.Match(ctx, i0))

	// Frame 1: drops
	i1 := makeVideoFrame(1000)
	testifyassert.False(t, f.Match(ctx, i1))

	// Frame 2 with NoPtsValue: passes, lastSentPTS is valid but this PTS is NoPtsValue
	i2 := makeVideoFrame(astiav.NoPtsValue)
	// frame with NoPtsValue: num=0 check? No, frame ID 2, eachN=2, remainder(0,2)=0 → pass
	// lastSentPTS is 0 (from frame 0), this PTS is NoPtsValue → large duration gap handling
	testifyassert.True(t, f.Match(ctx, i2))
}

func TestReduceFramerate_Video_LargeDurationGap(t *testing.T) {
	ctx := context.Background()
	f := New(mathcondition.GetterStatic[globaltypes.Rational]{
		StaticValue: globaltypes.Rational{Num: 1, Den: 2},
	})

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)

	makeVideoFrame := func(pts int64) packetorframe.InputUnion {
		avFrame := astiav.AllocFrame()
		avFrame.SetPts(pts)
		avFrame.SetDuration(1000)
		input := frame.Input{
			Frame: avFrame,
			StreamInfo: &frame.StreamInfo{
				CodecParameters: cp,
			},
		}
		return packetorframe.InputUnion{Frame: &input}
	}

	// Frame 0: passes
	i0 := makeVideoFrame(0)
	testifyassert.True(t, f.Match(ctx, i0))

	// Frame 1: drops
	i1 := makeVideoFrame(1000)
	testifyassert.False(t, f.Match(ctx, i1))

	// Frame 2: passes, with a very large PTS gap
	// duration = PTS(100000) - lastSentPTS(0) = 100000
	// expectedDuration = 1000 * 2 / 1 = 2000
	// 100000 > 2000*2 → duration = expectedDuration = 2000
	i2 := makeVideoFrame(100000)
	testifyassert.True(t, f.Match(ctx, i2))
	testifyassert.Equal(t, int64(2000), i2.Frame.Frame.Duration())
}

func TestReduceFramerate(t *testing.T) {
	loggerLevel := logger.LevelTrace

	runtime.DefaultCallerPCFilter = observability.CallerPCFilter(runtime.DefaultCallerPCFilter)
	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	t.Run("audio", func(t *testing.T) {
		f := New(mathcondition.GetterStatic[globaltypes.Rational]{
			StaticValue: globaltypes.Rational{
				Num: 3,
				Den: 7,
			},
		})

		frame := frame.Input{
			Frame: astiav.AllocFrame(),
			StreamInfo: &frame.StreamInfo{
				CodecParameters: astiav.AllocCodecParameters(),
			},
		}
		frame.StreamInfo.CodecParameters.SetMediaType(astiav.MediaTypeAudio)
		in := packetorframe.InputUnion{
			Frame: &frame,
		}

		for i := range 10 { // frames 0..69
			require.True(t, f.Match(ctx, in), i)  // (i*7+0) % 7 -> 0 % 2.(3) = 0.0   <  1 -> pass
			require.False(t, f.Match(ctx, in), i) // (i*7+1) % 7 -> 1 % 2.(3) = 1.0   >= 1 -> drop
			require.False(t, f.Match(ctx, in), i) // (i*7+2) % 7 -> 2 % 2.(3) = 2.0   >= 1 -> drop
			require.True(t, f.Match(ctx, in), i)  // (i*7+3) % 7 -> 3 % 2.(3) = 0.(6) <  1 -> pass
			require.False(t, f.Match(ctx, in), i) // (i*7+4) % 7 -> 4 % 2.(3) = 1.(6) >= 1 -> drop
			require.True(t, f.Match(ctx, in), i)  // (i*7+5) % 7 -> 5 % 2.(3) = 0.(3) <  1 -> pass
			require.False(t, f.Match(ctx, in), i) // (i*7+6) % 7 -> 6 % 2.(3) = 1.(3) >= 1 -> drop
		}
	})
}
