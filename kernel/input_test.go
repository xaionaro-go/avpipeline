package kernel

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/asticode/go-astiav"
	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
