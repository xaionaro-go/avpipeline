package kernel

import (
	"context"
	"image"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestAVFilterGraph_Overlay(t *testing.T) {
	ctx := context.Background()

	// 1. Define TrackConfigs for background and overlay
	config := map[int]avfilter.TrackConfig{
		0: {
			CodecParameters: astiav.AllocCodecParameters(),
			TimeBase:        astiav.NewRational(1, 30),
		},
		1: {
			CodecParameters: astiav.AllocCodecParameters(),
			TimeBase:        astiav.NewRational(1, 30),
			NoOutput:        true,
		},
	}
	defer config[0].CodecParameters.Free()
	defer config[1].CodecParameters.Free()

	// Setup background stream (HD)
	config[0].CodecParameters.SetMediaType(astiav.MediaTypeVideo)
	config[0].CodecParameters.SetWidth(1920)
	config[0].CodecParameters.SetHeight(1080)
	config[0].CodecParameters.SetPixelFormat(astiav.PixelFormatYuv420P)

	// Setup overlay stream (Small square)
	config[1].CodecParameters.SetMediaType(astiav.MediaTypeVideo)
	config[1].CodecParameters.SetWidth(200)
	config[1].CodecParameters.SetHeight(200)
	config[1].CodecParameters.SetPixelFormat(astiav.PixelFormatYuv420P)

	// 2. Create the graph with the overlay filter
	// labels in0 and in1 are automatically created by NewGraph for steam indices 0 and 1
	filterComplex := "[in0][in1]overlay=10:10[out0]"
	g, err := avfilter.NewGraph(ctx, config, filterComplex)
	require.NoError(t, err)
	defer g.Close()

	k := NewAVFilterGraph(ctx, g)
	defer k.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 10)

	// 3. Create frames
	f0 := astiav.AllocFrame()
	f0.SetWidth(1920)
	f0.SetHeight(1080)
	f0.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(t, f0.AllocBuffer(32))

	f1 := astiav.AllocFrame()
	f1.SetWidth(200)
	f1.SetHeight(200)
	f1.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(t, f1.AllocBuffer(32))

	// Fill frames with distinct colors
	fillFrame := func(f *astiav.Frame, y, u, v uint8) {
		img := image.NewYCbCr(image.Rect(0, 0, f.Width(), f.Height()), image.YCbCrSubsampleRatio420)
		for i := range img.Y {
			img.Y[i] = y
		}
		for i := range img.Cb {
			img.Cb[i] = u
		}
		for i := range img.Cr {
			img.Cr[i] = v
		}
		require.NoError(t, f.Data().FromImage(img))
	}
	fillFrame(f0, 100, 100, 100) // Background: Gray
	fillFrame(f1, 200, 200, 200) // Overlay: White

	// 4. Send frames to the graph
	input0 := packetorframe.InputUnion{
		Frame: &frame.Input{
			Frame: f0,
			StreamInfo: &frame.StreamInfo{
				StreamIndex: 0,
			},
		},
	}
	input1 := packetorframe.InputUnion{
		Frame: &frame.Input{
			Frame: f1,
			StreamInfo: &frame.StreamInfo{
				StreamIndex: 1,
			},
		},
	}

	// First input frame (background)
	err = k.SendInput(ctx, input0, outputCh)
	require.NoError(t, err)

	// Second input frame (overlay)
	err = k.SendInput(ctx, input1, outputCh)
	require.NoError(t, err)

	// 5. Verify output
	select {
	case out := <-outputCh:
		require.NotNil(t, out.Frame)
		testifyassert.Equal(t, 0, out.Frame.StreamIndex)
		testifyassert.Equal(t, 1920, out.Frame.Width())
		testifyassert.Equal(t, 1080, out.Frame.Height())

		// Verify pixels
		img := image.NewYCbCr(image.Rect(0, 0, 1920, 1080), image.YCbCrSubsampleRatio420)
		require.NoError(t, out.Frame.Data().ToImage(img))

		// Background pixel
		testifyassert.Equal(t, uint8(100), img.Y[img.YOffset(0, 0)])
		// Overlay pixel (at 10:10, size 200x200, so 15:15 is inside)
		testifyassert.Equal(t, uint8(200), img.Y[img.YOffset(15, 15)])
		// Overlay pixel (at 209:209 is still inside 10+200-1=209)
		testifyassert.Equal(t, uint8(200), img.Y[img.YOffset(209, 209)])
		// Background pixel (at 211:211 is outside)
		testifyassert.Equal(t, uint8(100), img.Y[img.YOffset(211, 211)])

	default:
		t.Fatal("expected output frame from overlay")
	}
}
