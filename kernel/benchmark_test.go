package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
)

func benchmarkGapFiller(b *testing.B, strategy GapsStrategyVideo, gapSize int64) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyVideo: strategy,
		},
	}
	state := &gapFillerStreamState{
		NextExpectedPTS:    0,
		NextExpectedPTSSet: true,
	}
	si := &frame.StreamInfo{
		TimeBase: astiav.NewRational(1, 1000),
	}

	// Prepare input frame
	inputFrame := astiav.AllocFrame()
	defer inputFrame.Free()
	inputFrame.SetTimeBase(astiav.NewRational(1, 1000))
	inputFrame.SetPts(gapSize)
	inputFrame.SetDuration(10)
	inputFrame.SetWidth(1280) // 720p is enough for benchmark
	inputFrame.SetHeight(720)
	inputFrame.SetPixelFormat(astiav.PixelFormatYuv420P)
	if err := inputFrame.AllocBuffer(0); err != nil {
		b.Fatal(err)
	}

	// If interpolate, we need a last frame
	if strategy == GapsStrategyVideoInterpolate {
		lastFrame := frame.CloneAsReferenced(inputFrame)
		lastFrame.SetPts(0) // Should be before the gapSize
		lastFrame.SetTimeBase(astiav.NewRational(1, 1000))
		state.LastVideoFrame = lastFrame
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset state for each iteration to force gap filling
		state.NextExpectedPTS = 0
		state.NextExpectedPTSSet = true
		if strategy == GapsStrategyVideoInterpolate {
			if state.LastVideoFrame != nil {
				frame.Pool.Put(state.LastVideoFrame)
			}
			lastFrame := frame.CloneAsReferenced(inputFrame)
			lastFrame.SetPts(0) // Should be before the gapSize
			lastFrame.SetTimeBase(astiav.NewRational(1, 1000))
			state.LastVideoFrame = lastFrame
		}

		// Cloning if we suspect the kernel modifies it or for cleanup consistency
		// But here we can use the same inputFrame if we are careful.
		// fixVideoGapIfNeeded returns new frames, and might modify inputFrame (for Extend)

		results, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
		if err != nil {
			b.Fatal(err)
		}

		// Clean up results
		for _, f := range results {
			if f != inputFrame {
				frame.Pool.Put(f)
			}
		}
	}
}

func BenchmarkGapFillerDuplicate_SmallGap(b *testing.B) {
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	benchmarkGapFiller(b, GapsStrategyVideoDuplicateNextFrame, 20)
}

func BenchmarkGapFillerDuplicate_LargeGap(b *testing.B) {
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	benchmarkGapFiller(b, GapsStrategyVideoDuplicateNextFrame, 100)
}

func BenchmarkGapFillerInterpolate_SmallGap(b *testing.B) {
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	benchmarkGapFiller(b, GapsStrategyVideoInterpolate, 20)
}

func BenchmarkGapFillerInterpolate_LargeGap(b *testing.B) {
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	benchmarkGapFiller(b, GapsStrategyVideoInterpolate, 100)
}
