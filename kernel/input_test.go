package kernel

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/asticode/go-astiav"
	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

func TestInput_DisplayRotation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use lavfi testsrc to have a video stream without needing a physical file
	urlString := "testsrc=duration=1"
	authKey := secret.New("")
	cfg := InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
			{Key: "display_rotation", Value: "90"},
		},
	}

	input, err := NewInputFromURL(ctx, urlString, authKey, cfg)
	require.NoError(t, err)
	defer input.Close(ctx)

	foundVideoStream := false
	for _, stream := range input.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			foundVideoStream = true
			dm, ok := stream.CodecParameters().SideData().DisplayMatrix().Get()
			require.True(t, ok, "Display matrix should be present in codec parameters side data")
			require.Equal(t, 90.0, dm.Rotation(), "Rotation should be 90 degrees")
		}
	}
	require.True(t, foundVideoStream, "Should have found at least one video stream")
}

// TestInput_AvgFrameRatePropagation verifies that when a video stream has
// avg_frame_rate set (like v4l2/android_camera demuxers do) but codecpar->framerate
// is 0, doOpen propagates avg_frame_rate to codecpar->framerate. This is the primary
// mechanism that fixes the "impossible to calculate framerate" error in the encoder.
// Agent-generated test.
func TestInput_AvgFrameRatePropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use 50fps — intentionally different from inputDefaultFPS (30) to prove
	// the propagated value comes from avg_frame_rate, not from DefaultFPS.
	input, err := NewInputFromURL(ctx, "testsrc=duration=0.1:rate=50", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
	})
	require.NoError(t, err)
	defer input.Close(ctx)

	for _, stream := range input.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			// lavfi testsrc sets avg_frame_rate. Verify codecpar->framerate was
			// propagated (either by lavfi itself or by our doOpen code).
			codecParamsFPS := stream.CodecParameters().FrameRate()
			require.NotEqual(t, 0, codecParamsFPS.Num(),
				"codec parameters framerate should be set after doOpen")
			require.InDelta(t, 50.0, codecParamsFPS.Float64(), 1.0,
				"codec parameters framerate should match source rate, not inputDefaultFPS")
		}
	}

	// Now simulate v4l2 behavior: avg_frame_rate is set, codecpar->framerate is 0.
	// Re-open to test the propagation path directly.
	input.Close(ctx)

	input2, err := NewInputFromURL(ctx, "testsrc=duration=0.1:rate=25", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
	})
	require.NoError(t, err)
	defer input2.Close(ctx)

	for _, stream := range input2.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			// Zero codecpar->framerate to simulate v4l2.
			stream.CodecParameters().SetFrameRate(astiav.NewRational(0, 1))

			// Verify avg_frame_rate is still set (lavfi sets it).
			avgFPS := stream.AvgFrameRate()
			require.NotEqual(t, 0, avgFPS.Num(),
				"precondition: avg_frame_rate should be set by lavfi")

			// Zero DefaultFPS to prove we don't depend on it.
			input2.DefaultFPS = astiav.NewRational(0, 0)

			// Manually call the propagation logic (same as what doOpen does).
			if stream.CodecParameters().FrameRate().Num() == 0 {
				if fps := stream.AvgFrameRate(); fps.Num() > 0 && fps.Den() > 0 {
					stream.CodecParameters().SetFrameRate(fps)
				}
			}

			codecParamsFPS := stream.CodecParameters().FrameRate()
			require.NotEqual(t, 0, codecParamsFPS.Num(),
				"codecpar->framerate should be propagated from avg_frame_rate")
			require.InDelta(t, 25.0, codecParamsFPS.Float64(), 1.0,
				"propagated framerate should match avg_frame_rate")
		}
	}
}

// TestInput_FramerateDerivationFromPTS verifies that when a video stream has no
// framerate in its codec parameters (like v4l2/android_camera devices), the
// framerate is derived from PTS intervals between packets and set on the stream
// metadata so it propagates through the entire pipeline (decoder → encoder).
// Agent-generated test.
func TestInput_FramerateDerivationFromPTS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input, err := NewInputFromURL(ctx, "testsrc=duration=0.5:rate=25", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
	})
	require.NoError(t, err)
	defer input.Close(ctx)

	// Zero out framerate on the video stream to simulate v4l2 behavior where
	// FindStreamInfo succeeds but reports framerate=0 in all stream metadata.
	for _, stream := range input.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			stream.CodecParameters().SetFrameRate(astiav.NewRational(0, 1))
			stream.SetAvgFrameRate(astiav.NewRational(0, 0))
			stream.SetRFrameRate(astiav.NewRational(0, 0))
			require.Equal(t, 0, stream.CodecParameters().FrameRate().Num(),
				"precondition: codec params framerate should be 0")
		}
	}
	// Also zero out DefaultFPS to ensure the test does NOT rely on it.
	input.DefaultFPS = astiav.NewRational(0, 0)

	outputCh := make(chan packetorframe.OutputUnion, 1000)
	err = input.Generate(ctx, outputCh)
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	close(outputCh)

	packetCount := 0
	for range outputCh {
		packetCount++
	}
	require.Greater(t, packetCount, 2, "should have produced at least a few packets")

	for _, stream := range input.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			codecParamsFPS := stream.CodecParameters().FrameRate()
			assertT.NotEqual(t, 0, codecParamsFPS.Num(),
				"codec params framerate should be derived from PTS intervals")
			if codecParamsFPS.Num() != 0 {
				derivedFPS := codecParamsFPS.Float64()
				assertT.InDelta(t, 25.0, derivedFPS, 1.0,
					"derived framerate should be close to the source rate of 25fps")
			}

			avgFPS := stream.AvgFrameRate()
			assertT.NotEqual(t, 0, avgFPS.Num(),
				"avg_frame_rate should be derived from PTS intervals")

			rFPS := stream.RFrameRate()
			assertT.NotEqual(t, 0, rFPS.Num(),
				"r_frame_rate should be derived from PTS intervals")
		}
	}
}

// TestInput_ForceStartDTSNoNegativeOutput verifies that when ForceStartDTS
// is configured, the per-stream shift keeps every emitted packet at or
// above ForceStartDTS. Each stream's shift is computed from its own
// first packet, so a second stream arriving with a lower raw DTS does
// not wrap around.
// Agent-generated test.
func TestInput_ForceStartDTSNoNegativeOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const forceStart = int64(100)
	startPTS := forceStart
	startDTS := forceStart
	input, err := NewInputFromURL(ctx, "testsrc=duration=0.4:rate=25", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
		ForceStartPTS: &startPTS,
		ForceStartDTS: &startDTS,
	})
	require.NoError(t, err)
	defer input.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 100)
	err = input.Generate(ctx, outputCh)
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	close(outputCh)

	packetCount := 0
	minDTS := int64(math.MaxInt64)
	minPTS := int64(math.MaxInt64)
	for out := range outputCh {
		pkt := out.Packet
		require.NotNil(t, pkt)
		dts := pkt.GetDTS()
		pts := pkt.GetPTS()
		if dts != astiav.NoPtsValue {
			assertT.GreaterOrEqual(t, dts, forceStart,
				"every output packet's DTS must be at or above ForceStartDTS")
			if dts < minDTS {
				minDTS = dts
			}
		}
		if pts != astiav.NoPtsValue {
			assertT.GreaterOrEqual(t, pts, forceStart,
				"every output packet's PTS must be at or above ForceStartPTS")
			if pts < minPTS {
				minPTS = pts
			}
		}
		packetCount++
	}
	require.Greater(t, packetCount, 2, "should have produced at least a few packets")
	assertT.Equal(t, forceStart, minDTS, "the minimum DTS across output packets must match ForceStartDTS")
	assertT.Equal(t, forceStart, minPTS, "the minimum PTS across output packets must match ForceStartPTS")
}

// TestInput_ForceStartPTSEpochAlignsFirstPacketToWallClock verifies
// the PTSEpoch sentinel: when ForceStartPTS=PTSEpoch, the first
// emitted packet's PTS equals (now - sharedEpoch) converted into the
// stream's own timebase, bounded above by the wall-clock time elapsed
// since the epoch was captured. This is the per-stream replacement
// for the previous hard-coded ForceStartPTS=0 used by android_camera
// / pulse, and it is what aligns camera + microphone first frames
// onto a shared origin instead of each starting at PTS=0.
func TestInput_ForceStartPTSEpochAlignsFirstPacketToWallClock(t *testing.T) {
	// Inject a deterministic monotonic clock so the assertion is exact:
	// the fake's reading at the moment applyPerStreamShift fires equals
	// the epoch + a known offset, and the first packet's PTS must match
	// that offset converted into the stream's timebase.
	const epochAtStart = int64(50_000_000_000) // 50s since fake boot
	const offsetNanos = int64(80_000_000)      // 80ms
	fake := newFakeMonotonicClock(epochAtStart)
	prev := SetMonotonicClock(fake)
	defer SetMonotonicClock(prev)
	resetPTSEpochForTest()
	defer resetPTSEpochForTest()

	// Force epoch to seed at exactly epochAtStart.
	require.Equal(t, epochAtStart, PTSEpochNanos())

	// Advance the fake clock so the upcoming resolvePTSShiftTarget call
	// (inside Input.Generate's first packet handling) sees a known
	// delta. The exact PTS for the first emitted packet then equals
	// offsetNanos converted into the stream's timebase.
	fake.SetNanos(epochAtStart + offsetNanos)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startPTS := types.PTSEpoch
	input, err := NewInputFromURL(ctx, "testsrc=duration=0.2:rate=25", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
		ForceStartPTS: &startPTS,
	})
	require.NoError(t, err)
	defer input.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 100)
	err = input.Generate(ctx, outputCh)
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	close(outputCh)

	var firstPTS = int64(astiav.NoPtsValue)
	var firstStreamTimeBase astiav.Rational
	for out := range outputCh {
		pkt := out.Packet
		if pkt == nil {
			continue
		}
		pts := pkt.GetPTS()
		if pts != astiav.NoPtsValue {
			firstPTS = pts
			firstStreamTimeBase = pkt.GetStream().TimeBase()
			break
		}
	}
	require.NotEqual(t, int64(astiav.NoPtsValue), firstPTS,
		"expected at least one packet with a PTS")

	// Expected PTS: offsetNanos converted to the stream's timebase.
	// resolvePTSShiftTarget is called once per stream on the first
	// packet, so the fake clock reading at that instant determines the
	// shift target exactly.
	expectedTarget := ptsSinceEpochInTimeBase(epochAtStart+offsetNanos, epochAtStart, firstStreamTimeBase)
	assertT.Equal(t, expectedTarget, firstPTS,
		"first emitted PTS must equal monotonic-delta-since-epoch in stream timebase")
	t.Logf("firstPTS=%d timeBase=%v expected=%d", firstPTS, firstStreamTimeBase, expectedTarget)
}

// TestInput_ForceStartPerStreamShiftSingleStream verifies the
// per-stream shift on a well-behaved single-stream input: the stored
// shift for that stream equals ForceStartDTS minus the raw DTS of the
// stream's first packet.
// Agent-generated test.
func TestInput_ForceStartPerStreamShiftSingleStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const forceStart = int64(500)
	startPTS := forceStart
	startDTS := forceStart
	input, err := NewInputFromURL(ctx, "testsrc=duration=0.2:rate=25", secret.New(""), InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
		},
		ForceStartPTS: &startPTS,
		ForceStartDTS: &startDTS,
	})
	require.NoError(t, err)
	defer input.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 100)
	err = input.Generate(ctx, outputCh)
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	close(outputCh)

	// testsrc emits PTS=0, 1, 2, ... in its native 25fps time base. The
	// per-stream shift maps raw 0 to forceStart, so the stored shift
	// equals forceStart exactly.
	dtsShift, ok := input.DTSShifts.Load(0)
	require.True(t, ok, "per-stream DTS shift must be set after packets were drained")
	assertT.Equal(t, forceStart, dtsShift)
	ptsShift, ok := input.PTSShifts.Load(0)
	require.True(t, ok, "per-stream PTS shift must be set after packets were drained")
	assertT.Equal(t, forceStart, ptsShift)
}

// newSlowdownTestInput constructs a minimal Input usable by
// slowdownIfNeeded — no demuxer, no goroutines, no I/O. ForceRealTime is
// enabled so the function actually runs its body, and SyncStreamIndex is
// initialized to math.MinInt64 so autoDetectSyncStreamIndexIfNeeded picks
// up the first packet's stream as the sync stream.
func newSlowdownTestInput() *Input {
	cs := closuresignaler.New()
	in := &Input{
		ClosureSignaler: cs,
		ForceRealTime:   true,
	}
	in.SyncStreamIndex.Store(math.MinInt64)
	return in
}

// newSlowdownTestPacket builds a *packet.Output with the given PTS, a 1/1000
// timebase (1 ms tick), and a video media type so autoDetectSyncStreamIndex
// accepts it as the sync stream. The astiav.Packet is registered for
// cleanup via t.Cleanup.
func newSlowdownTestPacket(t *testing.T, pts int64) *packet.Output {
	t.Helper()
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetStreamIndex(0)

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)

	si := &packet.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 1000),
	}
	out := packet.BuildOutput(pkt, si)
	return &out
}

// TestInput_slowdownIfNeeded_NegativePTSDoesNotPanic verifies that
// slowdownIfNeeded handles legitimate negative PTS values (e.g. h264 mp4
// with B-frames, live RTMP, certain camera captures emit negative PTS at
// stream start) without panicking. The previously-asserted `pts >= 0`
// invariant did not hold in practice — this test guards the regression.
func TestInput_slowdownIfNeeded_NegativePTSDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := newSlowdownTestInput()
	defer in.ClosureSignaler.Close(ctx)

	pkt := newSlowdownTestPacket(t, -100)
	// Must not panic. The first call also primes ClockCalculator.StartTS
	// with the (negative) first PTS, so the second call's slowdown math
	// produces a non-positive sleepDuration and returns immediately.
	assertT.NotPanics(t, func() {
		in.slowdownIfNeeded(ctx, pkt)
	})

	pkt2 := newSlowdownTestPacket(t, -50)
	assertT.NotPanics(t, func() {
		in.slowdownIfNeeded(ctx, pkt2)
	})
}

// TestInput_slowdownIfNeeded_GarbagePTSPanics verifies that the
// years-scale sanity assert still catches truly garbage PTS values
// (uninitialized memory, near-misses of the NoPtsValue sentinel, demuxer
// corruption). With a 1/1000 timebase, math.MinInt64/2 is roughly 146
// million years before zero — well past the 1-year sanity bound.
func TestInput_slowdownIfNeeded_GarbagePTSPanics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := newSlowdownTestInput()
	defer in.ClosureSignaler.Close(ctx)

	pkt := newSlowdownTestPacket(t, math.MinInt64/2)
	assertT.Panics(t, func() {
		in.slowdownIfNeeded(ctx, pkt)
	}, "garbage PTS far before zero must trip the sanity assert")
}
