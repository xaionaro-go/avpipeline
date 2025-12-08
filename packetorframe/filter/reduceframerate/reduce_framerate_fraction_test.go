package reduceframerate

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

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
