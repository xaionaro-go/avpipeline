package subpixelshift

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGlobalMotionFieldReturnsSameDXDYEverywhere(t *testing.T) {
	mf := newGlobalMotionField(1.5, -2.3, 0.9)

	assert.True(t, mf.isGlobal)
	assert.InDelta(t, 0.9, mf.confidence, 1e-9)

	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, 1.5, dx, 1e-9, "dx at (%d,%d)", x, y)
			assert.InDelta(t, -2.3, dy, 1e-9, "dy at (%d,%d)", x, y)
		}
	}
}

func TestPerPixelMotionFieldReturnsCorrectValues(t *testing.T) {
	fieldX := [][]float64{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	fieldY := [][]float64{
		{1.1, 1.2, 1.3},
		{1.4, 1.5, 1.6},
	}
	mf := newPerPixelMotionField(fieldX, fieldY, 0.8)

	assert.False(t, mf.isGlobal)
	assert.InDelta(t, 0.8, mf.confidence, 1e-9)

	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, fieldX[y][x], dx, 1e-9, "dx at (%d,%d)", x, y)
			assert.InDelta(t, fieldY[y][x], dy, 1e-9, "dy at (%d,%d)", x, y)
		}
	}
}

func TestPerPixelMotionFieldClampsOutOfBounds(t *testing.T) {
	fieldX := [][]float64{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	fieldY := [][]float64{
		{1.1, 1.2, 1.3},
		{1.4, 1.5, 1.6},
	}
	mf := newPerPixelMotionField(fieldX, fieldY, 0.7)

	// Beyond right edge: clamps x to 2.
	dx, dy := mf.displacementAt(5, 0)
	assert.InDelta(t, 0.3, dx, 1e-9)
	assert.InDelta(t, 1.3, dy, 1e-9)

	// Beyond bottom edge: clamps y to 1.
	dx, dy = mf.displacementAt(0, 10)
	assert.InDelta(t, 0.4, dx, 1e-9)
	assert.InDelta(t, 1.4, dy, 1e-9)

	// Negative coordinates: clamps to 0.
	dx, dy = mf.displacementAt(-1, -1)
	assert.InDelta(t, 0.1, dx, 1e-9)
	assert.InDelta(t, 1.1, dy, 1e-9)

	// Both beyond: clamps to bottom-right corner.
	dx, dy = mf.displacementAt(100, 100)
	assert.InDelta(t, 0.6, dx, 1e-9)
	assert.InDelta(t, 1.6, dy, 1e-9)
}

func TestZeroMotionFieldReturnsZeros(t *testing.T) {
	mf := zeroMotionField()

	assert.True(t, mf.isGlobal)
	assert.InDelta(t, 0.0, mf.confidence, 1e-9)

	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, 0.0, dx, 1e-9)
			assert.InDelta(t, 0.0, dy, 1e-9)
		}
	}
}
