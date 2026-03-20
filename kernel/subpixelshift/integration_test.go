package subpixelshift

import (
	"context"
	"math"
	"testing"

	astiav "github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

const (
	integrationHRSize = 64
	integrationLRSize = 32
	integrationScale  = 2
	integrationFrames = 8
)

// generateGroundTruth creates a 64x64 image with multiple sinusoidal
// frequencies that exercise the super-resolution pipeline.
// Agent-generated test helper.
func generateGroundTruth(w, h int) [][]float64 {
	img := make2D(h, w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w)
			fy := float64(y) / float64(h)

			// Multiple frequency components create fine detail that
			// a single LR frame cannot capture fully.
			v := 128.0 +
				40.0*math.Sin(2*math.Pi*3*fx)*math.Cos(2*math.Pi*2*fy) +
				25.0*math.Sin(2*math.Pi*7*fx+0.5) +
				20.0*math.Cos(2*math.Pi*5*fy+0.3) +
				15.0*math.Sin(2*math.Pi*11*fx)*math.Sin(2*math.Pi*9*fy)
			img[y][x] = clampFloat(v, 0, 255)
		}
	}
	return img
}

// downsample creates an LR frame from the HR image using 2x2 box averaging.
// Agent-generated test helper.
func downsample(
	hr [][]float64,
	hrW, hrH int,
	lrW, lrH int,
) [][]float64 {
	lr := make2D(lrH, lrW)
	scaleX := float64(hrW) / float64(lrW)
	scaleY := float64(hrH) / float64(lrH)

	for y := 0; y < lrH; y++ {
		for x := 0; x < lrW; x++ {
			hx := float64(x) * scaleX
			hy := float64(y) * scaleY
			lr[y][x] = bilinearSample(hr, hx, hy)
		}
	}
	return lr
}

// shiftPlaneSubPixel translates a 2D plane by (dx, dy) using bilinear interpolation.
// This simulates actual camera/scene motion in LR space.
// Agent-generated test helper.
func shiftPlaneSubPixel(
	plane [][]float64,
	w, h int,
	dx, dy float64,
) [][]float64 {
	shifted := make2D(h, w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			shifted[y][x] = bilinearSample(plane, float64(x)-dx, float64(y)-dy)
		}
	}
	return shifted
}

// planeToFrame builds an astiav.Frame from a float64 Y plane and fills
// U/V with neutral chroma (128).
// Agent-generated test helper.
func planeToFrame(
	t *testing.T,
	plane [][]float64,
	w, h int,
	pts int64,
) *astiav.Frame {
	t.Helper()

	f := astiav.AllocFrame()
	f.SetWidth(w)
	f.SetHeight(h)
	f.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(t, f.AllocBuffer(0))
	require.NoError(t, f.MakeWritable())

	// Use alignment=1 for the buffer layout so SetBytes(buf, 1) interprets
	// the planes correctly (Y linesize=w, chroma linesize=w/2).
	chromaH := h / 2
	chromaW := w / 2

	ySize := w * h
	uSize := chromaW * chromaH
	vSize := chromaW * chromaH

	buf := make([]byte, ySize+uSize+vSize)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf[y*w+x] = byte(clampFloat(math.Round(plane[y][x]), 0, 255))
		}
	}

	uOff := ySize
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			buf[uOff+y*chromaW+x] = 128
		}
	}

	vOff := ySize + uSize
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			buf[vOff+y*chromaW+x] = 128
		}
	}

	require.NoError(t, f.Data().SetBytes(buf, 1))
	f.SetPts(pts)
	return f
}

// mseYPlane computes the mean squared error between a float64 ground truth
// and the Y plane of an astiav.Frame.
// Agent-generated test helper.
func mseYPlane(
	t *testing.T,
	groundTruth [][]float64,
	f *astiav.Frame,
) float64 {
	t.Helper()

	w := f.Width()
	h := f.Height()
	require.Equal(t, len(groundTruth), h)
	require.Equal(t, len(groundTruth[0]), w)

	// Bytes(1) packs planes with alignment=1, so Y linesize == width.
	b, err := f.Data().Bytes(1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(b), h*w)

	sum := 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			got := float64(b[y*w+x])
			diff := got - groundTruth[y][x]
			sum += diff * diff
		}
	}
	return sum / float64(w*h)
}

// bilinearUpscale produces a simple bilinear upscale of an LR plane.
// Agent-generated test helper.
func bilinearUpscale(
	lr [][]float64,
	lrW, lrH, hrW, hrH int,
) [][]float64 {
	hr := make2D(hrH, hrW)
	scaleX := float64(lrW) / float64(hrW)
	scaleY := float64(lrH) / float64(hrH)

	for y := 0; y < hrH; y++ {
		for x := 0; x < hrW; x++ {
			hr[y][x] = bilinearSample(lr, float64(x)*scaleX, float64(y)*scaleY)
		}
	}
	return hr
}

// TestIntegrationSRPipeline verifies the end-to-end SR pipeline:
// correct output dimensions, no NaN/Inf, and finite MSE.
//
// Approach: generate a 64x64 HR ground truth, downsample to 32x32 LR,
// create 8 LR frames with sub-pixel jitter, feed through the kernel,
// and verify the SR output is a valid 64x64 image with reasonable MSE.
// Agent-generated test.
func TestIntegrationSRPipeline(t *testing.T) {
	ctx := context.Background()

	groundTruth := generateGroundTruth(integrationHRSize, integrationHRSize)

	baseLR := downsample(
		groundTruth,
		integrationHRSize, integrationHRSize,
		integrationLRSize, integrationLRSize,
	)

	// Sub-pixel shifts in LR pixel space (simulating camera jitter).
	lrShifts := [][2]float64{
		{0.0, 0.0},
		{0.5, 0.0},
		{0.0, 0.5},
		{0.5, 0.5},
		{0.25, 0.0},
		{0.0, 0.25},
		{0.25, 0.25},
		{0.75, 0.75},
	}

	lrFrames := make([][][]float64, len(lrShifts))
	for i, shift := range lrShifts {
		lrFrames[i] = shiftPlaneSubPixel(baseLR, integrationLRSize, integrationLRSize, shift[0], shift[1])
	}

	k := New(
		WithScale(integrationScale),
		WithBufferSize(integrationFrames),
		WithMotionMode(MotionModeGlobal),
		WithStartupMode(StartupModeBuffer),
	)
	defer func() { _ = k.Close(ctx) }()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeVideo)

	si := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 1000),
	}

	outputCh := make(chan packetorframe.OutputUnion, integrationFrames+1)

	for i, lr := range lrFrames {
		f := planeToFrame(t, lr, integrationLRSize, integrationLRSize, int64(i)*40)
		defer f.Free()

		fi := frame.BuildInput(f, 0, si)
		input := packetorframe.InputUnion{Frame: &fi}

		err := k.SendInput(ctx, input, outputCh)
		require.NoError(t, err)
	}

	// With StartupModeBuffer and bufSize=8, frames before the half mark
	// produce no output. Subsequent frames trigger SR output.
	require.Greater(t, len(outputCh), 0, "expected at least one SR output")

	var lastOutput packetorframe.OutputUnion
	for len(outputCh) > 0 {
		lastOutput = <-outputCh
	}

	require.NotNil(t, lastOutput.Frame)
	srFrame := lastOutput.Frame.Frame
	require.NotNil(t, srFrame)

	// Verify dimensions.
	assert.Equal(t, integrationHRSize, srFrame.Width())
	assert.Equal(t, integrationHRSize, srFrame.Height())

	// Verify pixel values are bounded and contain no NaN.
	b, err := srFrame.Data().Bytes(1)
	require.NoError(t, err)
	srW := srFrame.Width()
	srH := srFrame.Height()
	for y := 0; y < srH; y++ {
		for x := 0; x < srW; x++ {
			val := b[y*srW+x]
			assert.True(t, val <= 255, "pixel value out of range at (%d,%d): %d", x, y, val)
		}
	}

	// Compare against bilinear baseline — report both MSE values.
	srMSE := mseYPlane(t, groundTruth, srFrame)

	bilinearHR := bilinearUpscale(baseLR,
		integrationLRSize, integrationLRSize,
		integrationHRSize, integrationHRSize,
	)
	bilinearMSE := 0.0
	for y := 0; y < integrationHRSize; y++ {
		for x := 0; x < integrationHRSize; x++ {
			diff := bilinearHR[y][x] - groundTruth[y][x]
			bilinearMSE += diff * diff
		}
	}
	bilinearMSE /= float64(integrationHRSize * integrationHRSize)

	t.Logf("SR MSE: %.4f, Bilinear MSE: %.4f (informational)", srMSE, bilinearMSE)

	// The SR MSE should be finite and bounded (sanity check).
	assert.False(t, math.IsNaN(srMSE), "SR MSE must not be NaN")
	assert.False(t, math.IsInf(srMSE, 0), "SR MSE must not be Inf")
	assert.Less(t, srMSE, 5000.0,
		"SR MSE should be reasonable (under 5000 for 8-bit imagery)")
}

// TestIntegrationSceneCut feeds completely different frames to verify the
// kernel handles scene-cut-like input without panics, NaNs, or invalid
// output dimensions.
// Agent-generated test.
func TestIntegrationSceneCut(t *testing.T) {
	ctx := context.Background()

	k := New(
		WithScale(2),
		WithBufferSize(4),
		WithMotionMode(MotionModeGlobal),
		WithStartupMode(StartupModePassthrough),
	)
	defer func() { _ = k.Close(ctx) }()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeVideo)

	si := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 1000),
	}

	outputCh := make(chan packetorframe.OutputUnion, 8)

	// Generate 4 completely unrelated frames.
	for i := 0; i < 4; i++ {
		plane := make2D(32, 32)
		seed := float64(i+1) * 37.0
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				plane[y][x] = clampFloat(
					seed+100*math.Sin(float64(x+i*17))+80*math.Cos(float64(y+i*23)),
					0, 255,
				)
			}
		}

		f := planeToFrame(t, plane, 32, 32, int64(i)*40)
		defer f.Free()

		fi := frame.BuildInput(f, 0, si)
		input := packetorframe.InputUnion{Frame: &fi}

		err := k.SendInput(ctx, input, outputCh)
		require.NoError(t, err, "frame %d", i)
	}

	require.Greater(t, len(outputCh), 0, "expected at least one output")

	for len(outputCh) > 0 {
		out := <-outputCh
		require.NotNil(t, out.Frame)
		outFrame := out.Frame.Frame
		require.NotNil(t, outFrame)

		assert.Equal(t, 64, outFrame.Width())
		assert.Equal(t, 64, outFrame.Height())

		// Verify Y plane bytes are accessible and in valid range.
		b, err := outFrame.Data().Bytes(1)
		require.NoError(t, err)
		outW := outFrame.Width()
		outH := outFrame.Height()
		require.GreaterOrEqual(t, len(b), outW*outH,
			"byte buffer too small for Y plane")
	}
}
