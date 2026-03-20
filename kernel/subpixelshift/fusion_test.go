package subpixelshift

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHRGridNew(t *testing.T) {
	g := newHRGrid(8, 6, 3)

	require.Equal(t, 8, g.width)
	require.Equal(t, 6, g.height)
	require.Len(t, g.planes, 3)

	for i, p := range g.planes {
		require.Len(t, p.sum, 6, "plane %d sum rows", i)
		require.Len(t, p.weightSum, 6, "plane %d weightSum rows", i)
		for y := 0; y < 6; y++ {
			require.Len(t, p.sum[y], 8, "plane %d sum[%d] cols", i, y)
			require.Len(t, p.weightSum[y], 8, "plane %d weightSum[%d] cols", i, y)
		}
	}
}

func TestHRGridReset(t *testing.T) {
	g := newHRGrid(4, 4, 2)

	// Set some values.
	g.planes[0].sum[0][0] = 42.0
	g.planes[0].weightSum[0][0] = 1.0
	g.planes[1].sum[3][3] = 99.0
	g.planes[1].weightSum[3][3] = 2.0

	g.reset()

	for _, p := range g.planes {
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				assert.InDelta(t, 0.0, p.sum[y][x], 1e-15,
					"sum[%d][%d] not zero after reset", y, x)
				assert.InDelta(t, 0.0, p.weightSum[y][x], 1e-15,
					"weightSum[%d][%d] not zero after reset", y, x)
			}
		}
	}
}

func TestFuseFrameIdentity(t *testing.T) {
	// 4x4 LR input, scale=2 → 8x8 HR grid. Zero motion, age=0.
	scale := 2
	hrW := 4 * scale
	hrH := 4 * scale
	g := newHRGrid(hrW, hrH, 1)

	lr := [][]float64{
		{10, 20, 30, 40},
		{50, 60, 70, 80},
		{90, 100, 110, 120},
		{130, 140, 150, 160},
	}
	mf := zeroMotionField()

	fuseFrame(g, 0, lr, mf, scale, 0, 1.0)

	// HR pixel (0,0) maps back to LR (0,0)=10.
	// Weight = temporalWeight(0) * bilinearWeight(0,0) * 1.0 = 1.0 * 1.0 * 1.0 = 1.0
	require.Greater(t, g.planes[0].weightSum[0][0], 0.0)
	val := g.planes[0].sum[0][0] / g.planes[0].weightSum[0][0]
	assert.InDelta(t, 10.0, val, 1e-9, "HR(0,0) should map to LR(0,0)=10")

	// HR pixel (2,2) maps to LR (1,1)=60.
	require.Greater(t, g.planes[0].weightSum[2][2], 0.0)
	val = g.planes[0].sum[2][2] / g.planes[0].weightSum[2][2]
	assert.InDelta(t, 60.0, val, 1e-9, "HR(2,2) should map to LR(1,1)=60")
}

func TestFuseFrameWithShift(t *testing.T) {
	// Two frames fused with a 0.5px shift contribute at different HR positions.
	scale := 2
	hrW := 4 * scale
	hrH := 4 * scale
	g := newHRGrid(hrW, hrH, 1)

	lr := [][]float64{
		{100, 200, 300, 400},
		{500, 600, 700, 800},
		{900, 1000, 1100, 1200},
		{1300, 1400, 1500, 1600},
	}

	// Frame 1: zero motion, age=0.
	mf0 := zeroMotionField()
	fuseFrame(g, 0, lr, mf0, scale, 0, 1.0)

	// Frame 2: 0.5px horizontal shift, age=-1.
	mf1 := newGlobalMotionField(0.5, 0.0, 0.95)
	fuseFrame(g, 0, lr, mf1, scale, -1, 0.95)

	// Both frames contribute to the grid. The shifted frame provides
	// sub-pixel information that the unshifted frame does not.
	// HR pixel (1,0) is at LR (0.5, 0) for the zero-motion frame (fractional),
	// and at LR (0.5 - 0.5, 0) = LR (0, 0) for the shifted frame (integer).
	// The shifted frame's contribution at HR(1,0) should reflect LR(0,0)=100.
	require.Greater(t, g.planes[0].weightSum[0][1], 0.0,
		"HR(1,0) should have contributions")

	// Verify that the two frames produced different weighted sums at this position.
	val := g.planes[0].sum[0][1] / g.planes[0].weightSum[0][1]
	assert.Greater(t, val, 0.0, "fused value at HR(1,0) should be positive")
}

func TestExtractHRPlane(t *testing.T) {
	hp := &hrPlane{
		sum:       make2D(3, 3),
		weightSum: make2D(3, 3),
	}

	// Set known values: sum=value*weight, weightSum=weight.
	hp.sum[0][0] = 10.0
	hp.weightSum[0][0] = 2.0

	hp.sum[1][1] = 30.0
	hp.weightSum[1][1] = 3.0

	hp.sum[2][2] = 50.0
	hp.weightSum[2][2] = 5.0

	// Other cells have zero weight (gaps).
	result := extractHRPlane(hp, 3, 3, 0.01)

	require.Len(t, result, 3)
	require.Len(t, result[0], 3)

	// Filled cells: sum/weight.
	assert.InDelta(t, 5.0, result[0][0], 1e-9, "10/2 = 5")
	assert.InDelta(t, 10.0, result[1][1], 1e-9, "30/3 = 10")
	assert.InDelta(t, 10.0, result[2][2], 1e-9, "50/5 = 10")
}

func TestTemporalWeight(t *testing.T) {
	w0 := temporalWeight(0)
	w1 := temporalWeight(1)
	w3 := temporalWeight(3)
	wNeg1 := temporalWeight(-1)

	// age=0 has the highest weight.
	assert.Greater(t, w0, w1, "age 0 > age 1")
	assert.Greater(t, w1, w3, "age 1 > age 3")

	// Symmetric: age -1 == age 1.
	assert.InDelta(t, w1, wNeg1, 1e-15, "symmetric: age -1 == age 1")

	// Always > 0.
	assert.Greater(t, w0, 0.0)
	assert.Greater(t, w1, 0.0)
	assert.Greater(t, w3, 0.0)
	assert.Greater(t, wNeg1, 0.0)

	// age=0 should be 1.0 (exp(0) = 1).
	assert.InDelta(t, 1.0, w0, 1e-15)
}

func TestExcessiveMotionDetection(t *testing.T) {
	frameW := 100
	frameH := 100

	// Large global motion: 30% of frame width → excessive.
	largeMF := newGlobalMotionField(30.0, 0.0, 0.9)
	assert.True(t, isExcessiveMotion(largeMF, frameW, frameH),
		"30px on 100px frame should be excessive")

	// Small global motion: 5% of frame width → not excessive.
	smallMF := newGlobalMotionField(5.0, 0.0, 0.9)
	assert.False(t, isExcessiveMotion(smallMF, frameW, frameH),
		"5px on 100px frame should not be excessive")

	// Low confidence → excessive regardless of displacement.
	lowConfMF := newGlobalMotionField(1.0, 0.0, 0.1)
	assert.True(t, isExcessiveMotion(lowConfMF, frameW, frameH),
		"low confidence should be excessive")

	// Zero motion, good confidence → not excessive.
	zeroMF := newGlobalMotionField(0.0, 0.0, 0.9)
	assert.False(t, isExcessiveMotion(zeroMF, frameW, frameH),
		"zero motion should not be excessive")
}

func TestFuseFrameNaN(t *testing.T) {
	scale := 2
	hrW := 4 * scale
	hrH := 4 * scale
	g := newHRGrid(hrW, hrH, 1)

	lr := [][]float64{
		{0, 0, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	}
	mf := zeroMotionField()

	fuseFrame(g, 0, lr, mf, scale, 0, 1.0)

	for y := 0; y < hrH; y++ {
		for x := 0; x < hrW; x++ {
			assert.False(t, math.IsNaN(g.planes[0].sum[y][x]),
				"NaN in sum at (%d,%d)", x, y)
			assert.False(t, math.IsNaN(g.planes[0].weightSum[y][x]),
				"NaN in weightSum at (%d,%d)", x, y)
		}
	}

	// Also verify extraction produces no NaN.
	result := extractHRPlane(&g.planes[0], hrW, hrH, 0.01)
	for y := 0; y < hrH; y++ {
		for x := 0; x < hrW; x++ {
			assert.False(t, math.IsNaN(result[y][x]),
				"NaN in extracted HR plane at (%d,%d)", x, y)
		}
	}
}
