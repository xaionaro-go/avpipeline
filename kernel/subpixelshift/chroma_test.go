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

// planesToFrame builds an astiav.Frame from Y, U, V float64 planes.
// Agent-generated test helper.
func planesToFrame(
	t *testing.T,
	yPlane [][]float64,
	uPlane [][]float64,
	vPlane [][]float64,
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

	chromaW := w / 2
	chromaH := h / 2

	ySize := w * h
	uSize := chromaW * chromaH
	vSize := chromaW * chromaH

	buf := make([]byte, ySize+uSize+vSize)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf[y*w+x] = byte(clampFloat(math.Round(yPlane[y][x]), 0, 255))
		}
	}

	uOff := ySize
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			buf[uOff+y*chromaW+x] = byte(clampFloat(math.Round(uPlane[y][x]), 0, 255))
		}
	}

	vOff := ySize + uSize
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			buf[vOff+y*chromaW+x] = byte(clampFloat(math.Round(vPlane[y][x]), 0, 255))
		}
	}

	require.NoError(t, f.Data().SetBytes(buf, 1))
	f.SetPts(pts)
	return f
}

// generateChromaPlane creates a chroma plane with spatial variation for testing.
// Agent-generated test helper.
func generateChromaPlane(w, h int, baseVal, amplitude float64) [][]float64 {
	plane := make2D(h, w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w)
			fy := float64(y) / float64(h)
			v := baseVal + amplitude*math.Sin(2*math.Pi*3*fx)*math.Cos(2*math.Pi*2*fy)
			plane[y][x] = clampFloat(v, 0, 255)
		}
	}
	return plane
}

// Agent-generated test.
func TestExtractPlanesYUV420P(t *testing.T) {
	const w, h = 32, 32
	const chromaW, chromaH = 16, 16

	yPlane := generateGroundTruth(w, h)
	uPlane := generateChromaPlane(chromaW, chromaH, 120, 30)
	vPlane := generateChromaPlane(chromaW, chromaH, 140, 25)

	f := planesToFrame(t, yPlane, uPlane, vPlane, w, h, 0)
	defer f.Free()

	fi := frame.BuildInput(f, 0, nil)
	planes := extractPlanes(&fi)

	require.Len(t, planes, 3, "should extract 3 planes (Y, U, V)")

	// Y plane dimensions.
	assert.Equal(t, w, planes[0].width, "Y width")
	assert.Equal(t, h, planes[0].height, "Y height")
	require.Len(t, planes[0].data, h, "Y data rows")
	require.Len(t, planes[0].data[0], w, "Y data cols")

	// U plane dimensions (half res in 4:2:0).
	assert.Equal(t, chromaW, planes[1].width, "U width")
	assert.Equal(t, chromaH, planes[1].height, "U height")
	require.Len(t, planes[1].data, chromaH, "U data rows")
	require.Len(t, planes[1].data[0], chromaW, "U data cols")

	// V plane dimensions (half res in 4:2:0).
	assert.Equal(t, chromaW, planes[2].width, "V width")
	assert.Equal(t, chromaH, planes[2].height, "V height")
	require.Len(t, planes[2].data, chromaH, "V data rows")
	require.Len(t, planes[2].data[0], chromaW, "V data cols")

	// Verify Y values match input (within byte quantization).
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			expected := math.Round(clampFloat(yPlane[y][x], 0, 255))
			assert.InDelta(t, expected, planes[0].data[y][x], 1.0,
				"Y mismatch at (%d,%d)", x, y)
		}
	}

	// Verify U values match input.
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			expected := math.Round(clampFloat(uPlane[y][x], 0, 255))
			assert.InDelta(t, expected, planes[1].data[y][x], 1.0,
				"U mismatch at (%d,%d)", x, y)
		}
	}

	// Verify V values match input.
	for y := 0; y < chromaH; y++ {
		for x := 0; x < chromaW; x++ {
			expected := math.Round(clampFloat(vPlane[y][x], 0, 255))
			assert.InDelta(t, expected, planes[2].data[y][x], 1.0,
				"V mismatch at (%d,%d)", x, y)
		}
	}
}

// Agent-generated test.
func TestSROutputChromaDimensions(t *testing.T) {
	ctx := context.Background()

	const lrW, lrH = 32, 32
	const scale = 2
	const hrW, hrH = lrW * scale, lrH * scale
	const chromaHRW, chromaHRH = hrW / 2, hrH / 2

	k := New(
		WithScale(scale),
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

	yPlane := generateGroundTruth(lrW, lrH)
	uPlane := generateChromaPlane(lrW/2, lrH/2, 120, 30)
	vPlane := generateChromaPlane(lrW/2, lrH/2, 140, 25)

	f := planesToFrame(t, yPlane, uPlane, vPlane, lrW, lrH, 0)
	defer f.Free()

	fi := frame.BuildInput(f, 0, si)
	input := packetorframe.InputUnion{Frame: &fi}

	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)
	require.Equal(t, 1, len(outputCh))

	out := <-outputCh
	require.NotNil(t, out.Frame)
	outFrame := out.Frame.Frame
	require.NotNil(t, outFrame)

	assert.Equal(t, hrW, outFrame.Width(), "output width")
	assert.Equal(t, hrH, outFrame.Height(), "output height")

	// Verify the output buffer is large enough to hold Y + U + V.
	b, err := outFrame.Data().Bytes(1)
	require.NoError(t, err)

	ySize := hrW * hrH
	uSize := chromaHRW * chromaHRH
	vSize := chromaHRW * chromaHRH
	require.GreaterOrEqual(t, len(b), ySize+uSize+vSize,
		"output buffer too small for Y+U+V")
}

// Agent-generated test.
func TestSROutputChromaNotAllNeutral(t *testing.T) {
	ctx := context.Background()

	const lrW, lrH = 32, 32
	const scale = 2
	const frames = 8

	k := New(
		WithScale(scale),
		WithBufferSize(int32(frames)),
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

	outputCh := make(chan packetorframe.OutputUnion, frames+1)

	// Create frames with non-trivial chroma that varies spatially.
	lrShifts := [][2]float64{
		{0.0, 0.0}, {0.5, 0.0}, {0.0, 0.5}, {0.5, 0.5},
		{0.25, 0.0}, {0.0, 0.25}, {0.25, 0.25}, {0.75, 0.75},
	}

	baseY := generateGroundTruth(lrW, lrH)
	baseU := generateChromaPlane(lrW/2, lrH/2, 100, 40)
	baseV := generateChromaPlane(lrW/2, lrH/2, 160, 35)

	for i, shift := range lrShifts {
		shiftedY := shiftPlaneSubPixel(baseY, lrW, lrH, shift[0], shift[1])
		// Chroma shifts are half the luma shifts in 4:2:0.
		shiftedU := shiftPlaneSubPixel(baseU, lrW/2, lrH/2, shift[0]/2, shift[1]/2)
		shiftedV := shiftPlaneSubPixel(baseV, lrW/2, lrH/2, shift[0]/2, shift[1]/2)

		f := planesToFrame(t, shiftedY, shiftedU, shiftedV, lrW, lrH, int64(i)*40)
		defer f.Free()

		fi := frame.BuildInput(f, 0, si)
		input := packetorframe.InputUnion{Frame: &fi}

		err := k.SendInput(ctx, input, outputCh)
		require.NoError(t, err)
	}

	require.Greater(t, len(outputCh), 0, "expected at least one SR output")

	var lastOutput packetorframe.OutputUnion
	for len(outputCh) > 0 {
		lastOutput = <-outputCh
	}

	require.NotNil(t, lastOutput.Frame)
	srFrame := lastOutput.Frame.Frame
	require.NotNil(t, srFrame)

	hrW := lrW * scale
	hrH := lrH * scale
	chromaHRW := hrW / 2
	chromaHRH := hrH / 2

	b, err := srFrame.Data().Bytes(1)
	require.NoError(t, err)

	ySize := hrW * hrH
	uStart := ySize
	vStart := ySize + chromaHRW*chromaHRH

	// Count how many U/V chroma bytes differ from neutral 128.
	uNonNeutral := 0
	for i := uStart; i < vStart; i++ {
		if b[i] != 128 {
			uNonNeutral++
		}
	}

	vNonNeutral := 0
	vEnd := vStart + chromaHRW*chromaHRH
	for i := vStart; i < vEnd; i++ {
		if b[i] != 128 {
			vNonNeutral++
		}
	}

	totalChromaPixels := chromaHRW * chromaHRH

	// The input had spatially varying chroma (amplitude 40 and 35 around bases
	// 100 and 160), so the vast majority of output chroma pixels should differ
	// from neutral 128.
	assert.Greater(t, uNonNeutral, totalChromaPixels/2,
		"U plane should have many non-128 pixels (got %d/%d)", uNonNeutral, totalChromaPixels)
	assert.Greater(t, vNonNeutral, totalChromaPixels/2,
		"V plane should have many non-128 pixels (got %d/%d)", vNonNeutral, totalChromaPixels)
}

// Agent-generated test.
func TestMotionFieldScaled(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		mf := newGlobalMotionField(4.0, -2.0, 0.9)
		scaled := mf.scaled(0.5)

		assert.True(t, scaled.isGlobal)
		assert.InDelta(t, 2.0, scaled.dx, 1e-9)
		assert.InDelta(t, -1.0, scaled.dy, 1e-9)
		assert.InDelta(t, 0.9, scaled.confidence, 1e-9)
	})

	t.Run("per_pixel", func(t *testing.T) {
		fieldX := [][]float64{
			{2.0, 4.0},
			{6.0, 8.0},
		}
		fieldY := [][]float64{
			{-1.0, -3.0},
			{-5.0, -7.0},
		}
		mf := newPerPixelMotionField(fieldX, fieldY, 0.8)
		scaled := mf.scaled(0.5)

		assert.False(t, scaled.isGlobal)
		assert.InDelta(t, 0.8, scaled.confidence, 1e-9)

		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				assert.InDelta(t, fieldX[y][x]*0.5, scaled.fieldX[y][x], 1e-9,
					"scaled fieldX at (%d,%d)", x, y)
				assert.InDelta(t, fieldY[y][x]*0.5, scaled.fieldY[y][x], 1e-9,
					"scaled fieldY at (%d,%d)", x, y)
			}
		}
	})

	t.Run("identity_scale", func(t *testing.T) {
		mf := newGlobalMotionField(3.0, 5.0, 0.7)
		scaled := mf.scaled(1.0)

		assert.InDelta(t, 3.0, scaled.dx, 1e-9)
		assert.InDelta(t, 5.0, scaled.dy, 1e-9)
	})
}

// Agent-generated test.
func TestBilinearFallbackChroma(t *testing.T) {
	ctx := context.Background()

	const lrW, lrH = 32, 32
	const scale = 2

	k := New(
		WithScale(scale),
		WithBufferSize(8),
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

	outputCh := make(chan packetorframe.OutputUnion, 4)

	yPlane := generateGroundTruth(lrW, lrH)
	uPlane := generateChromaPlane(lrW/2, lrH/2, 80, 50)
	vPlane := generateChromaPlane(lrW/2, lrH/2, 200, 40)

	f := planesToFrame(t, yPlane, uPlane, vPlane, lrW, lrH, 0)
	defer f.Free()

	fi := frame.BuildInput(f, 0, si)
	input := packetorframe.InputUnion{Frame: &fi}

	// First frame with StartupModePassthrough and bufSize=8 uses bilinear fallback.
	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)
	require.Equal(t, 1, len(outputCh))

	out := <-outputCh
	require.NotNil(t, out.Frame)
	outFrame := out.Frame.Frame

	hrW := lrW * scale
	hrH := lrH * scale
	chromaHRW := hrW / 2
	chromaHRH := hrH / 2

	b, bErr := outFrame.Data().Bytes(1)
	require.NoError(t, bErr)

	ySize := hrW * hrH
	uStart := ySize
	vStart := ySize + chromaHRW*chromaHRH

	// Bilinear fallback should produce non-neutral chroma since input
	// had U centered at 80 (not 128) and V centered at 200 (not 128).
	uNonNeutral := 0
	for i := uStart; i < vStart; i++ {
		if b[i] != 128 {
			uNonNeutral++
		}
	}

	vNonNeutral := 0
	vEnd := vStart + chromaHRW*chromaHRH
	for i := vStart; i < vEnd; i++ {
		if b[i] != 128 {
			vNonNeutral++
		}
	}

	totalChromaPixels := chromaHRW * chromaHRH
	assert.Greater(t, uNonNeutral, totalChromaPixels/2,
		"bilinear fallback U should not be all neutral")
	assert.Greater(t, vNonNeutral, totalChromaPixels/2,
		"bilinear fallback V should not be all neutral")
}

// Agent-generated test.
func TestBufferedFrameYPlane(t *testing.T) {
	t.Run("with_planes", func(t *testing.T) {
		bf := &bufferedFrame{
			planes: []planeData{
				{data: [][]float64{{1, 2}, {3, 4}}, width: 2, height: 2},
				{data: [][]float64{{5}}, width: 1, height: 1},
				{data: [][]float64{{6}}, width: 1, height: 1},
			},
		}
		y := bf.yPlane()
		require.NotNil(t, y)
		assert.InDelta(t, 1.0, y[0][0], 1e-9)
		assert.InDelta(t, 4.0, y[1][1], 1e-9)
	})

	t.Run("no_planes", func(t *testing.T) {
		bf := &bufferedFrame{}
		assert.Nil(t, bf.yPlane())
	})
}
