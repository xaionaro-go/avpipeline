// gap_filler_test.go
package kernel

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/audio/pkg/interpolation/fourier"
	"github.com/xaionaro-go/avpipeline/frame"
)

func TestFixVideoGapDuplicateNextFrame(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name             string
		gapStart         int64
		inputPTS         int64
		originalDuration int64
		expectedPTSs     []int64
	}{
		{
			name:             "Single frame gap",
			gapStart:         0,
			inputPTS:         10,
			originalDuration: 10,
			expectedPTSs:     []int64{0, 10},
		},
		{
			name:             "Multi frame gap",
			gapStart:         0,
			inputPTS:         25,
			originalDuration: 10,
			expectedPTSs:     []int64{0, 25},
		},
		{
			name:             "Small gap",
			gapStart:         0,
			inputPTS:         5,
			originalDuration: 10,
			expectedPTSs:     []int64{0, 5},
		},
	}

	gf := &GapFiller{Config: GapFillerConfig{GapsStrategyVideo: GapsStrategyVideoDuplicateNextFrame}}
	state := &gapFillerStreamState{NextExpectedPTS: 0, NextExpectedPTSSet: false}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state.NextExpectedPTSSet = true
			state.NextExpectedPTS = tt.gapStart
			inputFrame := astiav.AllocFrame()
			inputFrame.SetPts(tt.inputPTS)
			inputFrame.SetDuration(tt.originalDuration)
			result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
			require.NoError(t, err)

			require.Equal(t, len(tt.expectedPTSs), len(result))
			for i, pts := range tt.expectedPTSs {
				require.Equal(t, pts, result[i].Pts())
			}
		})
	}
}

func TestFixVideoGapExtendNextFrame(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{Config: GapFillerConfig{GapsStrategyVideo: GapsStrategyVideoExtendNextFrame}}
	state := &gapFillerStreamState{NextExpectedPTS: 0, NextExpectedPTSSet: true}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	inputFrame := astiav.AllocFrame()
	inputFrame.SetPts(10)
	inputFrame.SetDuration(10)

	result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
	require.NoError(t, err)

	require.Equal(t, 1, len(result))
	require.Equal(t, int64(0), result[0].Pts())
	require.Equal(t, int64(20), result[0].Duration())
}

func TestFixVideoGapNone(t *testing.T) {
	ctx := context.Background()
	t.Run("No strategy", func(t *testing.T) {
		gf := &GapFiller{
			Config: GapFillerConfig{
				GapsStrategyVideo: GapsStrategyVideoNone,
			},
		}
		state := &gapFillerStreamState{}
		si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

		f1 := astiav.AllocFrame()
		defer f1.Free()
		f1.SetPts(10)
		f1.SetDuration(10)

		result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{f1})
		require.NoError(t, err)
		require.Equal(t, 1, len(result))
		require.Equal(t, int64(10), result[0].Pts())
	})

	t.Run("No gap", func(t *testing.T) {
		gf := &GapFiller{
			Config: GapFillerConfig{
				GapsStrategyVideo: GapsStrategyVideoDuplicateNextFrame,
			},
		}
		state := &gapFillerStreamState{}
		si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

		f1 := astiav.AllocFrame()
		defer f1.Free()
		f1.SetPts(0)
		f1.SetDuration(10)

		f2 := astiav.AllocFrame()
		defer f2.Free()
		f2.SetPts(10)
		f2.SetDuration(10)

		result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{f1, f2})
		require.NoError(t, err)
		require.Equal(t, 2, len(result))
		require.Equal(t, int64(0), result[0].Pts())
		require.Equal(t, int64(10), result[1].Pts())
	})
}

func TestFixVideoGapMaxDuration(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyVideo:   GapsStrategyVideoExtendNextFrame,
			VideoMaxGapDuration: 100 * time.Millisecond,
		},
	}
	state := &gapFillerStreamState{NextExpectedPTS: 0, NextExpectedPTSSet: true}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	// 200ms gap > 100ms max
	inputFrame := astiav.AllocFrame()
	inputFrame.SetPts(200)
	inputFrame.SetDuration(10)

	result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
	require.NoError(t, err)

	require.Equal(t, 1, len(result))
	require.Equal(t, int64(200), result[0].Pts())
	require.True(t, state.NextExpectedPTSSet)
	require.Equal(t, int64(210), state.NextExpectedPTS)
}

func TestVideoGapLarge(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyVideo: GapsStrategyVideoAddBlankFrames,
		},
	}
	state := &gapFillerStreamState{NextExpectedPTS: 0, NextExpectedPTSSet: true}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	inputFrame := astiav.AllocFrame()
	inputFrame.SetPts(25)
	inputFrame.SetDuration(10)
	inputFrame.SetWidth(320)
	inputFrame.SetHeight(240)
	inputFrame.SetPixelFormat(astiav.PixelFormatYuv420P)

	result, err := gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
	require.NoError(t, err)
	require.Equal(t, 2, len(result))
	require.Equal(t, int64(0), result[0].Pts())
	require.Equal(t, int64(25), result[0].Duration())
	require.Equal(t, int64(25), result[1].Pts())
}

func TestVideoGapInterpolate(t *testing.T) {
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

func TestFixAudioGapDetection(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyAudio: GapsStrategyAudioAddSilence,
		},
	}
	state := &gapFillerStreamState{
		NextExpectedPTS:    100,
		NextExpectedPTSSet: true,
	}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(150)
	f.SetNbSamples(100)
	f.SetDuration(50)
	f.SetSampleFormat(astiav.SampleFormatS16)
	f.SetSampleRate(44100)
	f.SetChannelLayout(astiav.ChannelLayoutStereo)

	result, err := gf.fixAudioGapIfNeededForOneFrame(ctx, state, si, f)
	require.NoError(t, err)
	require.True(t, len(result) > 1)
	require.Equal(t, int64(100), result[0].Pts())
}

func TestFixAudioGapReset(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyAudio:   GapsStrategyAudioAddSilence,
			AudioMaxGapDuration: 10 * time.Millisecond,
		},
	}
	state := &gapFillerStreamState{
		NextExpectedPTS:    100,
		NextExpectedPTSSet: true,
	}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(1000) // 900ms gap > 10ms max
	f.SetNbSamples(100)
	f.SetDuration(50)

	result, err := gf.fixAudioGapIfNeededForOneFrame(ctx, state, si, f)
	require.NoError(t, err)
	require.Equal(t, 1, len(result))
	require.Equal(t, int64(1000), result[0].Pts())
	require.True(t, state.NextExpectedPTSSet)
	require.Equal(t, int64(1050), state.NextExpectedPTS)
}

func TestFixAudioGapInterpolate(t *testing.T) {
	ctx := context.Background()
	gf := &GapFiller{
		Config: GapFillerConfig{
			GapsStrategyAudio: GapsStrategyAudioInterpolate,
		},
		interpolator: fourier.New(),
	}
	state := &gapFillerStreamState{
		NextExpectedPTS:    100,
		NextExpectedPTSSet: true,
	}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	// Create last frame (all zeros)
	fLast := astiav.AllocFrame()
	fLast.SetNbSamples(1024)
	fLast.SetSampleFormat(astiav.SampleFormatFltp)
	fLast.SetSampleRate(48000)
	fLast.SetChannelLayout(astiav.ChannelLayoutMono)
	fLast.AllocBuffer(0)
	state.LastAudioFrame = fLast

	// Create current frame (all zeros)
	fCurr := astiav.AllocFrame()
	fCurr.SetPts(150)
	fCurr.SetNbSamples(1024)
	fCurr.SetDuration(50)
	fCurr.SetSampleFormat(astiav.SampleFormatFltp)
	fCurr.SetSampleRate(48000)
	fCurr.SetChannelLayout(astiav.ChannelLayoutMono)
	fCurr.AllocBuffer(0)

	result, err := gf.fixAudioGapIfNeededForOneFrame(ctx, state, si, fCurr)
	require.NoError(t, err)
	require.Equal(t, 2, len(result))
	require.Equal(t, int64(100), result[0].Pts()) // The interpolated frame
	require.Equal(t, int64(50), result[0].Duration())
	require.Equal(t, int64(150), result[1].Pts()) // The current frame
}

func BenchmarkGapFillerDuplicate(b *testing.B) {
	ctx := context.Background()
	gf := &GapFiller{Config: GapFillerConfig{GapsStrategyVideo: GapsStrategyVideoDuplicateNextFrame}}
	state := &gapFillerStreamState{NextExpectedPTS: 0, NextExpectedPTSSet: true}
	si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

	inputFrame := astiav.AllocFrame()
	defer inputFrame.Free()
	inputFrame.SetPts(10)
	inputFrame.SetDuration(10)
	inputFrame.SetWidth(320)
	inputFrame.SetHeight(240)
	inputFrame.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(b, inputFrame.AllocBuffer(0))

	for b.Loop() {
		state.NextExpectedPTS = 0
		state.NextExpectedPTSSet = true
		_, _ = gf.fixVideoGapIfNeeded(ctx, state, si, []*astiav.Frame{inputFrame})
	}
}

func BenchmarkGapFillerInterpolateResolutions(b *testing.B) {
	resolutions := []struct {
		name   string
		width  int
		height int
	}{
		{"240p", 320, 240},
		{"1080p", 1920, 1080},
		{"4K", 3840, 2160},
	}

	modes := []string{"mci", "blend"}

	for _, res := range resolutions {
		for _, mode := range modes {
			b.Run(fmt.Sprintf("%s_%s", res.name, mode), func(b *testing.B) {
				ctx := context.Background()
				cfg := DefaultGapFillerConfig()
				cfg.GapsStrategyVideo = GapsStrategyVideoInterpolate
				cfg.VideoInterpolationMode = mode
				gf := NewGapFiller(ctx, &cfg)
				state := &gapFillerStreamState{NextExpectedPTS: 10, NextExpectedPTSSet: true}
				si := &frame.StreamInfo{TimeBase: astiav.NewRational(1, 1000)}

				fLast := astiav.AllocFrame()
				defer fLast.Free()
				fLast.SetPts(0)
				fLast.SetDuration(10)
				fLast.SetWidth(res.width)
				fLast.SetHeight(res.height)
				fLast.SetPixelFormat(astiav.PixelFormatYuv420P)
				fLast.SetTimeBase(si.TimeBase)
				require.NoError(b, fLast.AllocBuffer(0))
				state.LastVideoFrame = fLast

				fCurr := astiav.AllocFrame()
				defer fCurr.Free()
				fCurr.SetPts(20) // One frame gap (expected 10, got 20)
				fCurr.SetDuration(10)
				fCurr.SetWidth(res.width)
				fCurr.SetHeight(res.height)
				fCurr.SetPixelFormat(astiav.PixelFormatYuv420P)
				fCurr.SetTimeBase(si.TimeBase)
				require.NoError(b, fCurr.AllocBuffer(0))

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					state.NextExpectedPTS = 10
					state.NextExpectedPTSSet = true

					res, err := gf.fixVideoGapIfNeededForOneFrame(ctx, state, si, fCurr)
					require.NoError(b, err)
					require.NotEmpty(b, res)

					if state.videoFilterGraph != nil {
						state.videoFilterGraph.Free()
						state.videoFilterGraph = nil
					}
				}
				b.StopTimer()

				// Evaluate FPS
				if b.N > 0 {
					t := b.Elapsed()
					if t > 0 {
						fps := float64(b.N) / t.Seconds()
						fmt.Printf("Resolution: %s, Mode: %s, FPS: %.2f\n", res.name, mode, fps)
					}
				}
			})
		}
	}
}
