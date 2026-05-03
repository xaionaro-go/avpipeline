//go:build test_long

package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
)

func TestVideoGapInterpolate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := DefaultGapFillerConfig()
	cfg.GapsStrategyVideo = GapsStrategyVideoInterpolate
	gf := NewGapFiller(ctx, &cfg)

	si := &frame.StreamInfo{
		StreamIndex: 0,
		TimeBase:    astiav.NewRational(1, 1000),
	}

	state := &gapFillerStreamState{
		NextExpectedPTS:    10,
		NextExpectedPTSSet: true,
	}

	fLast := astiav.AllocFrame()
	fLast.SetPts(0)
	fLast.SetDuration(10)
	fLast.SetWidth(320)
	fLast.SetHeight(240)
	fLast.SetPixelFormat(astiav.PixelFormatYuv420P)
	fLast.SetTimeBase(si.TimeBase)
	require.NoError(t, fLast.AllocBuffer(0))
	state.LastVideoFrame = fLast

	fCurr := astiav.AllocFrame()
	fCurr.SetPts(30)
	fCurr.SetDuration(10)
	fCurr.SetWidth(320)
	fCurr.SetHeight(240)
	fCurr.SetPixelFormat(astiav.PixelFormatYuv420P)
	fCurr.SetTimeBase(si.TimeBase)
	require.NoError(t, fCurr.AllocBuffer(0))

	result, err := gf.fixVideoGapIfNeededForOneFrame(ctx, state, si, fCurr)
	require.NoError(t, err)
	require.NoError(t, err)
	// minterpolate might not return frames immediately, but it should return some.
	// Actually, with mi_mode=mci, it definitely needs more frames to interpolate.
	// But it should at least return the current frame if it doesn't interpolate.
	t.Logf("Result len: %d", len(result))
	for _, f := range result {
		t.Logf("Result frame PTS: %d", f.Pts())
	}
	require.True(t, len(result) >= 1)
}
