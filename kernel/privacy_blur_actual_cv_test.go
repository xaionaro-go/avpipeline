//go:build with_cv
// +build with_cv

package kernel

import (
	"context"
	"image"
	"sync/atomic"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"gocv.io/x/gocv"
)

// TestBlurRegions_GaussianBlur verifies that blurRegions actually modifies pixel
// data in the target rectangle using Gaussian blur.
func TestBlurRegions_GaussianBlur(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	// Create a 100x100 white mat with a black 40x40 square in the center.
	mat := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(255, 255, 255, 0), 100, 100, gocv.MatTypeCV8UC3)
	defer mat.Close()

	// Draw a black block in center (sharp edge → easily detectable change after blur).
	for y := 30; y < 70; y++ {
		for x := 30; x < 70; x++ {
			mat.SetUCharAt(y, x*3+0, 0)
			mat.SetUCharAt(y, x*3+1, 0)
			mat.SetUCharAt(y, x*3+2, 0)
		}
	}

	// Snapshot center pixels before blur.
	beforeEdge := mat.GetUCharAt(30, 30*3) // black pixel at edge

	// Blur the region containing the black square.
	rects := []image.Rectangle{image.Rect(20, 20, 80, 80)}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err)

	// After Gaussian blur, the sharp black→white edge at (30,30) should be softened.
	// The pixel that was 0 (black) should now have a higher value due to blurring with white neighbors.
	afterEdge := mat.GetUCharAt(30, 30*3)
	testifyassert.NotEqual(t, beforeEdge, afterEdge,
		"pixel at the edge of the black square should change after Gaussian blur")

	// Also verify pixels well outside the blur rect are unchanged.
	outsidePixel := mat.GetUCharAt(5, 5*3)
	testifyassert.Equal(t, uint8(255), outsidePixel, "pixels outside blur region should be unchanged")
}

// TestBlurRegions_Pixelation verifies that blurRegions pixelates when PixelateBlockSize > 1.
func TestBlurRegions_Pixelation(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	pb.BlurRadius.Store(0)
	pb.PixelateBlockSize.Store(10)

	// Create a gradient mat: each row has incrementing values.
	mat := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer mat.Close()

	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			v := uint8((x * 255) / 100)
			mat.SetUCharAt(y, x*3+0, v)
			mat.SetUCharAt(y, x*3+1, v)
			mat.SetUCharAt(y, x*3+2, v)
		}
	}

	// Snapshot some pixels before pixelation.
	before10 := mat.GetUCharAt(50, 10*3)
	before11 := mat.GetUCharAt(50, 11*3)

	// On a smooth gradient, adjacent pixels differ.
	testifyassert.NotEqual(t, before10, before11,
		"adjacent gradient pixels should differ before pixelation")

	rects := []image.Rectangle{image.Rect(0, 0, 100, 100)}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err)

	// After pixelation with block size 10, pixels within the same 10px block
	// should have the same value (nearest-neighbor resize down then up).
	after10 := mat.GetUCharAt(50, 10*3)
	after11 := mat.GetUCharAt(50, 11*3)
	testifyassert.Equal(t, after10, after11,
		"adjacent pixels within the same pixelation block should be equal after pixelation")
}

// TestBlurRegions_EmptyRect verifies that an empty rectangle is a no-op.
func TestBlurRegions_EmptyRect(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	mat := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(128, 128, 128, 0), 50, 50, gocv.MatTypeCV8UC3)
	defer mat.Close()

	before := mat.GetUCharAt(25, 25*3)

	// Empty rect (x1==x2).
	rects := []image.Rectangle{image.Rect(10, 10, 10, 10)}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err)

	after := mat.GetUCharAt(25, 25*3)
	testifyassert.Equal(t, before, after, "empty rect should not modify any pixels")
}

// TestBlurRegions_OutOfBoundsRect verifies that rects extending beyond mat bounds
// are clipped correctly and don't panic.
func TestBlurRegions_OutOfBoundsRect(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	mat := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(200, 200, 200, 0), 50, 50, gocv.MatTypeCV8UC3)
	defer mat.Close()

	// Rect extending beyond mat bounds.
	rects := []image.Rectangle{image.Rect(-10, -10, 60, 60)}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err, "out-of-bounds rect should be clipped, not panic")
}

// TestBlurRegions_MultipleRects verifies that multiple non-overlapping regions
// are all blurred.
func TestBlurRegions_MultipleRects(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	// Create a mat with two distinct colored blocks.
	mat := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(255, 255, 255, 0), 100, 200, gocv.MatTypeCV8UC3)
	defer mat.Close()

	// Black block on left.
	for y := 10; y < 40; y++ {
		for x := 10; x < 40; x++ {
			mat.SetUCharAt(y, x*3+0, 0)
			mat.SetUCharAt(y, x*3+1, 0)
			mat.SetUCharAt(y, x*3+2, 0)
		}
	}
	// Black block on right.
	for y := 60; y < 90; y++ {
		for x := 110; x < 140; x++ {
			mat.SetUCharAt(y, x*3+0, 0)
			mat.SetUCharAt(y, x*3+1, 0)
			mat.SetUCharAt(y, x*3+2, 0)
		}
	}

	beforeLeft := mat.GetUCharAt(10, 10*3)   // black
	beforeRight := mat.GetUCharAt(60, 110*3) // black

	rects := []image.Rectangle{
		image.Rect(5, 5, 45, 45),
		image.Rect(105, 55, 145, 95),
	}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err)

	afterLeft := mat.GetUCharAt(10, 10*3)
	afterRight := mat.GetUCharAt(60, 110*3)

	testifyassert.NotEqual(t, beforeLeft, afterLeft,
		"left region edge pixel should change after blur")
	testifyassert.NotEqual(t, beforeRight, afterRight,
		"right region edge pixel should change after blur")
}

// TestSendInput_EnabledProcessesFrame verifies the full SendInput path produces
// a valid output frame when enabled (even if no face is detected in a plain
// frame, the conversion pipeline itself runs successfully).
func TestSendInput_EnabledProcessesFrame(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())
	// Enabled=nil means always on.

	ctx := context.Background()
	input := newTestVideoInput(t, 320, 240)
	defer input.Frame.Frame.Free()
	defer input.Frame.CodecParameters.Free()
	outputCh := make(chan packetorframe.OutputUnion, 1)

	err := pb.SendInput(ctx, input, outputCh)
	require.NoError(t, err)
	require.Len(t, outputCh, 1)

	out := <-outputCh
	require.NotNil(t, out.Frame, "should produce an output frame")
	testifyassert.Equal(t, 320, out.Frame.Width())
	testifyassert.Equal(t, 240, out.Frame.Height())
}

// TestSendInput_DisabledVsEnabled verifies that disabled mode produces a
// passthrough while enabled mode processes the frame through the pipeline.
func TestSendInput_DisabledVsEnabled(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())

	ctx := context.Background()

	// Disabled: passthrough.
	enabled := &atomic.Bool{}
	enabled.Store(false)
	pb.Enabled = enabled

	input1 := newTestVideoInput(t, 64, 64)
	defer input1.Frame.Frame.Free()
	defer input1.Frame.CodecParameters.Free()
	ch1 := make(chan packetorframe.OutputUnion, 1)
	require.NoError(t, pb.SendInput(ctx, input1, ch1))
	out1 := <-ch1
	require.NotNil(t, out1.Frame)

	// Enabled: processes through pipeline.
	enabled.Store(true)
	input2 := newTestVideoInput(t, 64, 64)
	defer input2.Frame.Frame.Free()
	defer input2.Frame.CodecParameters.Free()
	ch2 := make(chan packetorframe.OutputUnion, 1)
	require.NoError(t, pb.SendInput(ctx, input2, ch2))
	out2 := <-ch2
	require.NotNil(t, out2.Frame)
}

// TestBlurRegions_GaussianReducesVariance creates a high-variance checkerboard
// pattern and verifies that Gaussian blur reduces the pixel variance (smoothing).
func TestBlurRegions_GaussianReducesVariance(t *testing.T) {
	pb := newTestPrivacyBlur(t)
	defer pb.Close(context.Background())
	pb.BlurRadius.Store(15)

	// Create a checkerboard pattern with high variance.
	mat := gocv.NewMatWithSize(80, 80, gocv.MatTypeCV8UC3)
	defer mat.Close()

	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			var v uint8
			if (x/4+y/4)%2 == 0 {
				v = 255
			} else {
				v = 0
			}
			mat.SetUCharAt(y, x*3+0, v)
			mat.SetUCharAt(y, x*3+1, v)
			mat.SetUCharAt(y, x*3+2, v)
		}
	}

	varianceBefore := computeVariance(&mat, image.Rect(10, 10, 70, 70))

	rects := []image.Rectangle{image.Rect(10, 10, 70, 70)}
	err := pb.blurRegions(context.Background(), &mat, rects)
	require.NoError(t, err)

	varianceAfter := computeVariance(&mat, image.Rect(10, 10, 70, 70))

	testifyassert.Less(t, varianceAfter, varianceBefore,
		"Gaussian blur should reduce pixel variance (smoothing): before=%.1f, after=%.1f",
		varianceBefore, varianceAfter)
}

// computeVariance computes the variance of the first channel in the given rect.
func computeVariance(mat *gocv.Mat, rect image.Rectangle) float64 {
	var sum, sumSq float64
	n := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			v := float64(mat.GetUCharAt(y, x*3))
			sum += v
			sumSq += v * v
			n++
		}
	}
	mean := sum / float64(n)
	return sumSq/float64(n) - mean*mean
}
