package subpixelshift

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPlane4x4 is a 4x4 plane: row i, col j has value i*4+j.
//
//	{{0,1,2,3},{4,5,6,7},{8,9,10,11},{12,13,14,15}}
func testPlane4x4() [][]float64 {
	return [][]float64{
		{0, 1, 2, 3},
		{4, 5, 6, 7},
		{8, 9, 10, 11},
		{12, 13, 14, 15},
	}
}

func TestBilinearSampleIntegerPositions(t *testing.T) {
	plane := testPlane4x4()

	// Each integer position should return the exact pixel value.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			got := bilinearSample(plane, float64(x), float64(y))
			assert.InDelta(t, plane[y][x], got, 1e-9,
				"at (%d,%d)", x, y)
		}
	}
}

func TestBilinearSampleMidpoints(t *testing.T) {
	plane := testPlane4x4()

	// Midpoint between (0,0)=0, (1,0)=1, (0,1)=4, (1,1)=5 => average = 2.5
	got := bilinearSample(plane, 0.5, 0.5)
	assert.InDelta(t, 2.5, got, 1e-9)

	// Midpoint between (1,1)=5, (2,1)=6, (1,2)=9, (2,2)=10 => average = 7.5
	got = bilinearSample(plane, 1.5, 1.5)
	assert.InDelta(t, 7.5, got, 1e-9)
}

func TestBilinearSampleEdgeInterpolation(t *testing.T) {
	plane := testPlane4x4()

	// Halfway along x at y=0: between (1,0)=1 and (2,0)=2 => 1.5
	got := bilinearSample(plane, 1.5, 0.0)
	assert.InDelta(t, 1.5, got, 1e-9)

	// Halfway along y at x=0: between (0,0)=0 and (0,1)=4 => 2.0
	got = bilinearSample(plane, 0.0, 0.5)
	assert.InDelta(t, 2.0, got, 1e-9)
}

func TestBilinearSampleOutOfBoundsClamps(t *testing.T) {
	plane := testPlane4x4()

	// Negative coordinates clamp to edge.
	got := bilinearSample(plane, -1.0, -1.0)
	assert.InDelta(t, plane[0][0], got, 1e-9)

	// Beyond max clamps to edge.
	got = bilinearSample(plane, 10.0, 10.0)
	assert.InDelta(t, plane[3][3], got, 1e-9)

	// Mixed: x negative, y beyond max.
	got = bilinearSample(plane, -0.5, 5.0)
	assert.InDelta(t, plane[3][0], got, 1e-9)
}

func TestBilinearWeightAtInteger(t *testing.T) {
	assert.InDelta(t, 1.0, bilinearWeight(0.0, 0.0), 1e-9)
	assert.InDelta(t, 1.0, bilinearWeight(1.0, 2.0), 1e-9)
	assert.InDelta(t, 1.0, bilinearWeight(-3.0, 7.0), 1e-9)
}

func TestBilinearWeightAtMidpoint(t *testing.T) {
	assert.InDelta(t, 0.25, bilinearWeight(0.5, 0.5), 1e-9)
	assert.InDelta(t, 0.25, bilinearWeight(1.5, 2.5), 1e-9)
}

func TestBilinearWeightOnEdge(t *testing.T) {
	// On one axis edge (0.5 on one axis, integer on other).
	assert.InDelta(t, 0.5, bilinearWeight(0.5, 0.0), 1e-9)
	assert.InDelta(t, 0.5, bilinearWeight(0.0, 0.5), 1e-9)
	assert.InDelta(t, 0.5, bilinearWeight(1.5, 1.0), 1e-9)
}

func TestParabolicPeakFitSymmetric(t *testing.T) {
	// Symmetric peak: center is max, neighbors are equal on each axis.
	dx, dy := parabolicPeakFit(3, 5, 3, 3, 5, 3)
	assert.InDelta(t, 0.0, dx, 1e-9)
	assert.InDelta(t, 0.0, dy, 1e-9)
}

func TestParabolicPeakFitAsymmetricShiftsTowardHigher(t *testing.T) {
	// Right neighbor larger than left => positive dx.
	dx, dy := parabolicPeakFit(2, 5, 4, 3, 5, 3)
	assert.Greater(t, dx, 0.0)
	assert.InDelta(t, 0.0, dy, 1e-9)

	// Bottom neighbor larger than top => positive dy.
	dx, dy = parabolicPeakFit(3, 5, 3, 2, 5, 4)
	assert.InDelta(t, 0.0, dx, 1e-9)
	assert.Greater(t, dy, 0.0)

	// Left neighbor larger => negative dx.
	dx, dy = parabolicPeakFit(4, 5, 2, 3, 5, 3)
	assert.Less(t, dx, 0.0)
	assert.InDelta(t, 0.0, dy, 1e-9)
}

func TestParabolicPeakFitClampedToHalf(t *testing.T) {
	// Extreme asymmetry: the result must be clamped to [-0.5, 0.5].
	dx, dy := parabolicPeakFit(0, 5, 100, 0, 5, 100)
	assert.LessOrEqual(t, math.Abs(dx), 0.5)
	assert.LessOrEqual(t, math.Abs(dy), 0.5)

	dx, dy = parabolicPeakFit(100, 5, 0, 100, 5, 0)
	assert.LessOrEqual(t, math.Abs(dx), 0.5)
	assert.LessOrEqual(t, math.Abs(dy), 0.5)
}

func TestParabolicPeakFitFlatReturnsZero(t *testing.T) {
	// All neighbors equal (flat, no parabola) => (0,0).
	dx, dy := parabolicPeakFit(5, 5, 5, 5, 5, 5)
	assert.InDelta(t, 0.0, dx, 1e-9)
	assert.InDelta(t, 0.0, dy, 1e-9)
}

func TestFillGapsCheckerboard(t *testing.T) {
	// Create a 4x4 checkerboard: filled cells have known values, gaps have zero weight.
	n := 4
	sum := make([][]float64, n)
	weightSum := make([][]float64, n)
	for y := 0; y < n; y++ {
		sum[y] = make([]float64, n)
		weightSum[y] = make([]float64, n)
		for x := 0; x < n; x++ {
			if (x+y)%2 == 0 {
				sum[y][x] = float64(y*n + x)
				weightSum[y][x] = 1.0
			}
		}
	}

	fillGaps(sum, weightSum, 0.01)

	// All gaps should now be filled (weight > threshold).
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			require.Greater(t, weightSum[y][x], 0.01,
				"gap at (%d,%d) not filled", x, y)
		}
	}

	// Filled values for gap cells should be reasonable (average of neighbors).
	// For the checkerboard center cell (1,1): neighbors are (0,1),(2,1),(1,0),(1,2).
	// (0,1) and (1,0) are filled: values 1 and 4. (2,1) and (1,2) are also filled: 9 and 6.
	// So gap (1,1) should get average of 1,4,9,6 = 5.0.
	val := sum[1][1] / weightSum[1][1]
	assert.InDelta(t, 5.0, val, 1e-9, "checkerboard center gap value")
}

func TestFillGapsNoGaps(t *testing.T) {
	// All cells filled: nothing changes.
	sum := [][]float64{{1, 2}, {3, 4}}
	ws := [][]float64{{1, 1}, {1, 1}}

	sumCopy := [][]float64{{1, 2}, {3, 4}}
	wsCopy := [][]float64{{1, 1}, {1, 1}}

	fillGaps(sum, ws, 0.01)

	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			assert.InDelta(t, sumCopy[y][x], sum[y][x], 1e-9)
			assert.InDelta(t, wsCopy[y][x], ws[y][x], 1e-9)
		}
	}
}
