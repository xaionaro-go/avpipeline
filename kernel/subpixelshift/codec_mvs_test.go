package subpixelshift

import (
	"testing"

	astiav "github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterpolateMVsToPerPixel(t *testing.T) {
	// Two 16x16 macroblocks side by side in a 32x16 frame.
	// Left block: centered at (8,8), motion (2.0, 0.0).
	// Right block: centered at (24,8), motion (0.0, 1.0).
	width := 32
	height := 16

	mvs := []astiav.MotionVector{
		{
			W:           16,
			H:           16,
			DstX:        8,
			DstY:        8,
			MotionX:     8,
			MotionY:     0,
			MotionScale: 4,
		},
		{
			W:           16,
			H:           16,
			DstX:        24,
			DstY:        8,
			MotionX:     0,
			MotionY:     4,
			MotionScale: 4,
		},
	}

	mf := interpolateMVsToField(mvs, width, height)

	require.False(t, mf.isGlobal)
	require.InDelta(t, 0.9, mf.confidence, 1e-9)

	// Left half: displacement should be (2.0, 0.0).
	for y := 0; y < height; y++ {
		for x := 0; x < 16; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, 2.0, dx, 1e-9, "left half dx at (%d,%d)", x, y)
			assert.InDelta(t, 0.0, dy, 1e-9, "left half dy at (%d,%d)", x, y)
		}
	}

	// Right half: displacement should be (0.0, 1.0).
	for y := 0; y < height; y++ {
		for x := 16; x < width; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, 0.0, dx, 1e-9, "right half dx at (%d,%d)", x, y)
			assert.InDelta(t, 1.0, dy, 1e-9, "right half dy at (%d,%d)", x, y)
		}
	}
}

func TestInterpolateMVsEmpty(t *testing.T) {
	// nil MVs → zero motion field.
	mf := interpolateMVsToField(nil, 16, 16)
	assert.True(t, mf.isGlobal)
	assert.InDelta(t, 0.0, mf.confidence, 1e-9)

	dx, dy := mf.displacementAt(0, 0)
	assert.InDelta(t, 0.0, dx, 1e-9)
	assert.InDelta(t, 0.0, dy, 1e-9)

	// Empty slice → zero motion field.
	mf = interpolateMVsToField([]astiav.MotionVector{}, 16, 16)
	assert.True(t, mf.isGlobal)
	assert.InDelta(t, 0.0, mf.confidence, 1e-9)
}

func TestInterpolateMVsQuarterPixel(t *testing.T) {
	// Single 16x16 macroblock in a 16x16 frame with quarter-pixel motion.
	// MotionX=1, MotionScale=4 → 0.25px horizontal displacement.
	width := 16
	height := 16

	mvs := []astiav.MotionVector{
		{
			W:           16,
			H:           16,
			DstX:        8,
			DstY:        8,
			MotionX:     1,
			MotionY:     0,
			MotionScale: 4,
		},
	}

	mf := interpolateMVsToField(mvs, width, height)

	require.False(t, mf.isGlobal)

	// Every pixel should have dx=0.25, dy=0.0.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx, dy := mf.displacementAt(x, y)
			assert.InDelta(t, 0.25, dx, 1e-9, "dx at (%d,%d)", x, y)
			assert.InDelta(t, 0.0, dy, 1e-9, "dy at (%d,%d)", x, y)
		}
	}
}
