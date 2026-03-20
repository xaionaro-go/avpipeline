package subpixelshift

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestPlane creates a test image with rich spatial structure.
// Combines multiple sinusoidal frequencies with a deterministic per-pixel
// hash for uniqueness, giving the phase correlator enough structure to
// lock onto.
func makeTestPlane(width, height int) [][]float64 {
	plane := make([][]float64, height)
	for y := range plane {
		row := make([]float64, width)
		for x := range row {
			xf := float64(x)
			yf := float64(y)

			// Multiple spatial frequencies for directional sensitivity.
			v := 30*math.Sin(xf*0.5) +
				20*math.Cos(yf*0.7) +
				25*math.Sin(xf*0.3+yf*0.4) +
				15*math.Cos(xf*1.1-yf*0.8) +
				10*math.Sin(xf*2.0+yf*1.5)

			// Deterministic per-pixel "hash" for spatial uniqueness.
			// Uses a simple integer hash based on coordinates.
			h := uint32(x*73856093 ^ y*19349663)
			h = (h >> 13) ^ h
			h *= 0x85ebca6b
			v += float64(h%256) * 0.04

			row[x] = v
		}
		plane[y] = row
	}
	return plane
}

// shiftPlane creates an integer-pixel shifted copy of plane using circular
// (wrap-around) shifting. This matches the FFT's assumption of periodicity
// and avoids introducing zero-border artifacts.
func shiftPlane(plane [][]float64, dx, dy int) [][]float64 {
	h := len(plane)
	if h == 0 {
		return nil
	}
	w := len(plane[0])

	out := make([][]float64, h)
	for y := range out {
		row := make([]float64, w)
		for x := range row {
			srcX := ((x - dx) % w + w) % w
			srcY := ((y - dy) % h + h) % h
			row[x] = plane[srcY][srcX]
		}
		out[y] = row
	}
	return out
}

func TestPhaseCorrelationZeroShift(t *testing.T) {
	plane := makeTestPlane(64, 64)

	mf, err := estimateGlobalMotion(plane, plane)
	require.NoError(t, err)

	assert.True(t, mf.isGlobal, "motion field should be global")
	assert.InDelta(t, 0, mf.dx, 0.5, "dx should be near 0")
	assert.InDelta(t, 0, mf.dy, 0.5, "dy should be near 0")
	assert.Greater(t, mf.confidence, 0.5, "confidence should be high for identical images")
}

func TestPhaseCorrelationIntegerShift(t *testing.T) {
	ref := makeTestPlane(64, 64)

	tests := []struct {
		name     string
		shiftDX  int
		shiftDY  int
	}{
		{"right3", 3, 0},
		{"down2", 0, 2},
		{"diag1_1", 1, 1},
		{"neg2_neg3", -2, -3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shifted := shiftPlane(ref, tc.shiftDX, tc.shiftDY)

			mf, err := estimateGlobalMotion(ref, shifted)
			require.NoError(t, err)

			assert.True(t, mf.isGlobal, "motion field should be global")
			assert.InDelta(t, float64(tc.shiftDX), mf.dx, 0.5,
				"dx: expected %d, got %f", tc.shiftDX, mf.dx)
			assert.InDelta(t, float64(tc.shiftDY), mf.dy, 0.5,
				"dy: expected %d, got %f", tc.shiftDY, mf.dy)
		})
	}
}

func TestPhaseCorrelationTooSmall(t *testing.T) {
	plane := makeTestPlane(2, 2)

	_, err := estimateGlobalMotion(plane, plane)
	require.Error(t, err, "should reject images smaller than 8x8")
}

func TestHannWindow(t *testing.T) {
	w := hannWindow(32, 32)

	// Corners should be near zero.
	assert.Less(t, w[0][0], 0.01, "top-left corner should be near 0")
	assert.Less(t, w[0][31], 0.01, "top-right corner should be near 0")
	assert.Less(t, w[31][0], 0.01, "bottom-left corner should be near 0")
	assert.Less(t, w[31][31], 0.01, "bottom-right corner should be near 0")

	// Center values should be high.
	center := w[16][16]
	assert.Greater(t, center, 0.5, "center should be > 0.5")

	// All values should be in [0, 1].
	for y := range w {
		for x := range w[y] {
			assert.GreaterOrEqual(t, w[y][x], 0.0)
			assert.LessOrEqual(t, w[y][x], 1.0)
		}
	}
}

func TestFFTRoundtrip(t *testing.T) {
	original := []complex128{1, 2, 3, 4, 5, 6, 7, 8}

	data := make([]complex128, len(original))
	copy(data, original)

	fft(data)

	// After FFT the data should differ from original (except trivial cases).
	differs := false
	for i := range data {
		if cmplx.Abs(data[i]-original[i]) > 1e-9 {
			differs = true
			break
		}
	}
	assert.True(t, differs, "FFT output should differ from input")

	ifft(data)

	// After IFFT we should recover the original.
	for i := range data {
		assert.InDelta(t, real(original[i]), real(data[i]), 1e-9,
			"real part mismatch at index %d", i)
		assert.InDelta(t, imag(original[i]), imag(data[i]), 1e-9,
			"imag part mismatch at index %d", i)
	}
}
