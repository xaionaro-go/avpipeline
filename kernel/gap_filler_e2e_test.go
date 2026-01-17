package kernel

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func readFramesFromFile(t *testing.T, filePath string, mediaType astiav.MediaType, maxFrames int) ([]*astiav.Frame, *frame.StreamInfo) {
	fmtFormatCtx := astiav.AllocFormatContext()
	require.NotNil(t, fmtFormatCtx)
	defer fmtFormatCtx.Free()

	err := fmtFormatCtx.OpenInput(filePath, nil, nil)
	require.NoError(t, err)
	defer fmtFormatCtx.CloseInput()

	err = fmtFormatCtx.FindStreamInfo(nil)
	require.NoError(t, err)

	var stream *astiav.Stream
	for _, s := range fmtFormatCtx.Streams() {
		if s.CodecParameters().MediaType() == mediaType {
			stream = s
			break
		}
	}
	require.NotNil(t, stream)

	codec := astiav.FindDecoder(stream.CodecParameters().CodecID())
	require.NotNil(t, codec)
	codecCtx := astiav.AllocCodecContext(codec)
	require.NotNil(t, codecCtx)
	defer codecCtx.Free()
	err = stream.CodecParameters().ToCodecContext(codecCtx)
	require.NoError(t, err)
	err = codecCtx.Open(codec, nil)
	require.NoError(t, err)

	cp := astiav.AllocCodecParameters()
	err = stream.CodecParameters().Copy(cp)
	require.NoError(t, err)

	si := &frame.StreamInfo{
		TimeBase:        stream.TimeBase(),
		StreamIndex:     stream.Index(),
		CodecParameters: cp,
	}

	pkt := astiav.AllocPacket()
	defer pkt.Free()

	var allFrames []*astiav.Frame
	for len(allFrames) < maxFrames {
		err = fmtFormatCtx.ReadFrame(pkt)
		if err != nil {
			break
		}
		if pkt.StreamIndex() != stream.Index() {
			pkt.Unref()
			continue
		}
		err = codecCtx.SendPacket(pkt)
		require.NoError(t, err)
		pkt.Unref()

		for {
			f := astiav.AllocFrame()
			err = codecCtx.ReceiveFrame(f)
			if err == astiav.ErrEagain || err == astiav.ErrEof {
				f.Free()
				break
			}
			require.NoError(t, err)

			err = f.MakeWritable()
			if err != nil {
				f.Free()
				t.Fatalf("failed to make frame writable: %v", err)
			}
			allFrames = append(allFrames, f)
		}
	}

	// Ensure PTS and Duration are set
	frameDuration := int64(si.TimeBase.Den()) / int64(si.TimeBase.Num()) / 30
	if frameDuration == 0 {
		frameDuration = 1000
	}
	for i, f := range allFrames {
		f.SetTimeBase(si.TimeBase)
		f.SetPts(int64(i) * frameDuration)
		f.SetDuration(frameDuration)
	}

	return allFrames, si
}

func compareFrames(f1, f2 *astiav.Frame, plane int) float64 {
	if f1.Width() != f2.Width() || f1.Height() != f2.Height() {
		return math.MaxFloat64
	}

	var totalDiff int64
	d1, err := f1.Data().Bytes(plane)
	if err != nil {
		return math.MaxFloat64
	}
	d2, err := f2.Data().Bytes(plane)
	if err != nil {
		return math.MaxFloat64
	}
	ls1 := f1.Linesize()[plane]
	ls2 := f2.Linesize()[plane]

	h := f1.Height()
	w := f1.Width()
	if plane > 0 {
		// Simplified for YUV420
		h /= 2
		w /= 2
	}

	for y := range h {
		row1 := d1[y*ls1 : y*ls1+w]
		row2 := d2[y*ls2 : y*ls2+w]
		for x := range w {
			diff := int64(row1[x]) - int64(row2[x])
			totalDiff += diff * diff
		}
	}

	return math.Sqrt(float64(totalDiff) / float64(w*h))
}

func TestGapFillerE2E_FLV_Interpolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	allFrames, si := readFramesFromFile(t, "testdata/motion.flv", astiav.MediaTypeVideo, 20)
	require.GreaterOrEqual(t, len(allFrames), 15)

	cfg := DefaultGapFillerConfig()
	cfg.GapsStrategyVideo = GapsStrategyVideoInterpolate
	cfg.VideoInterpolationMode = "mci"
	gf := NewGapFiller(ctx, &cfg)

	outputCh := make(chan packetorframe.OutputUnion, 100)

	// Push first frame
	err := gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
		Frame:      allFrames[0],
		StreamInfo: si,
	}}, outputCh)
	require.NoError(t, err)

	var receivedFrames []*astiav.Frame
	select {
	case out := <-outputCh:
		receivedFrames = append(receivedFrames, out.Frame.Frame)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for first frame")
	}
	require.Len(t, receivedFrames, 1)

	// Create a large gap (10 frames)
	inputFrame := frame.CloneAsReferenced(allFrames[10])
	frameDuration := allFrames[0].Duration()
	inputFrame.SetPts(allFrames[0].Pts() + 10*frameDuration)

	// Push frame after gap
	err = gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
		Frame:      inputFrame,
		StreamInfo: si,
	}}, outputCh)
	require.NoError(t, err)

	// Pull results
LOOP:
	for {
		select {
		case out := <-outputCh:
			receivedFrames = append(receivedFrames, out.Frame.Frame)
		case <-time.After(20 * time.Second): // MCI is REALLY slow
			break LOOP
		}
	}

	require.GreaterOrEqual(t, len(receivedFrames), 2, "Should return at least the filled frame and the input frame")

	// Verify that at least one frame is an interpolation (not a duplicate)
	var interpolatedFrame *astiav.Frame
	for _, f := range receivedFrames {
		if f.Pts() > allFrames[0].Pts() && f.Pts() < inputFrame.Pts() {
			interpolatedFrame = f
			break
		}
	}
	require.NotNil(t, interpolatedFrame, "Interpolated frame not found")

	// Compare with nearest original frames
	diff0 := compareFrames(interpolatedFrame, allFrames[0], 0)
	diff10 := compareFrames(interpolatedFrame, allFrames[10], 0)

	t.Logf("MSE diff to frame 0: %f, to frame 10: %f", diff0, diff10)
	require.True(t, diff0 > 0.001, "Interpolated frame should not be identical to frame 0")
	require.True(t, diff10 > 0.001, "Interpolated frame should not be identical to frame 10")
}

func TestGapFillerE2E_VideoStrategiesCombined(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	allFrames, si := readFramesFromFile(t, "testdata/motion.flv", astiav.MediaTypeVideo, 5)

	strategies := []struct {
		strategy GapsStrategyVideo
		name     string
	}{
		{GapsStrategyVideoAddBlankFrames, "BlankFrames"},
		{GapsStrategyVideoDuplicateNextFrame, "Duplicate"},
		{GapsStrategyVideoExtendNextFrame, "Extend"},
	}

	for _, tc := range strategies {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultGapFillerConfig()
			cfg.GapsStrategyVideo = tc.strategy
			gf := NewGapFiller(ctx, &cfg)

			outputCh := make(chan packetorframe.OutputUnion, 100)

			// First frame
			err := gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
				Frame:      allFrames[0],
				StreamInfo: si,
			}}, outputCh)
			require.NoError(t, err)
			<-outputCh

			// Create a gap (2 frames)
			frameDuration := allFrames[0].Duration()
			f2 := frame.CloneAsReferenced(allFrames[1])
			f2.SetPts(allFrames[0].Pts() + 3*frameDuration) // PTS 0, expected 1, gap at 1 and 2, input at 3.

			err = gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
				Frame:      f2,
				StreamInfo: si,
			}}, outputCh)
			require.NoError(t, err)

			var receivedFrames []*astiav.Frame
		LOOP:
			for {
				select {
				case out := <-outputCh:
					receivedFrames = append(receivedFrames, out.Frame.Frame)
				case <-time.After(1 * time.Second):
					break LOOP
				}
			}

			if tc.strategy == GapsStrategyVideoExtendNextFrame {
				require.Len(t, receivedFrames, 1)
				require.Equal(t, 3*frameDuration, receivedFrames[0].Duration())
				require.Equal(t, allFrames[0].Pts()+frameDuration, receivedFrames[0].Pts())
			} else {
				require.GreaterOrEqual(t, len(receivedFrames), 2)
			}
		})
	}
}

func TestGapFillerE2E_AudioStrategies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	allFrames, si := readFramesFromFile(t, "testdata/audio.wav", astiav.MediaTypeAudio, 2)
	require.GreaterOrEqual(t, len(allFrames), 2)

	strategies := []struct {
		strategy GapsStrategyAudio
		name     string
	}{
		{GapsStrategyAudioAddSilence, "Silence"},
		{GapsStrategyAudioInterpolate, "Interpolate"},
	}

	for _, tc := range strategies {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultGapFillerConfig()
			cfg.GapsStrategyAudio = tc.strategy
			gf := NewGapFiller(ctx, &cfg)

			outputCh := make(chan packetorframe.OutputUnion, 100)

			err := gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
				Frame:      allFrames[0],
				StreamInfo: si,
			}}, outputCh)
			require.NoError(t, err)
			<-outputCh

			// Create a gap
			frameDuration := allFrames[0].Duration()
			f2 := frame.CloneAsReferenced(allFrames[1])
			f2.SetPts(allFrames[0].Pts() + 3*frameDuration)

			err = gf.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
				Frame:      f2,
				StreamInfo: si,
			}}, outputCh)
			require.NoError(t, err)

			var receivedFrames []*astiav.Frame
		LOOP:
			for {
				select {
				case out := <-outputCh:
					receivedFrames = append(receivedFrames, out.Frame.Frame)
				case <-time.After(1 * time.Second):
					break LOOP
				}
			}

			require.GreaterOrEqual(t, len(receivedFrames), 2)

			if tc.strategy == GapsStrategyAudioInterpolate {
				// Verify interpolation isn't all zeros
				hasNonZero := false
				data, err := receivedFrames[0].Data().Bytes(0)
				require.NoError(t, err)
				for _, v := range data {
					if v != 0 {
						hasNonZero = true
						break
					}
				}
				require.True(t, hasNonZero, "Interpolated audio should not be silent")
			}
		})
	}
}
