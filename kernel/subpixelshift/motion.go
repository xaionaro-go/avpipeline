package subpixelshift

// motionField represents estimated motion between two frames, either as a
// single global displacement or as per-pixel displacement fields.
type motionField struct {
	isGlobal   bool
	dx, dy     float64
	fieldX     [][]float64
	fieldY     [][]float64
	confidence float64
}

// newGlobalMotionField creates a motion field with uniform displacement everywhere.
func newGlobalMotionField(
	dx, dy, confidence float64,
) motionField {
	return motionField{
		isGlobal:   true,
		dx:         dx,
		dy:         dy,
		confidence: confidence,
	}
}

// newPerPixelMotionField creates a motion field with per-pixel displacement maps.
func newPerPixelMotionField(
	fieldX, fieldY [][]float64,
	confidence float64,
) motionField {
	return motionField{
		isGlobal:   false,
		fieldX:     fieldX,
		fieldY:     fieldY,
		confidence: confidence,
	}
}

// zeroMotionField returns a global motion field with zero displacement and zero confidence.
func zeroMotionField() motionField {
	return motionField{
		isGlobal:   true,
		confidence: 0,
	}
}

// displacementAt returns the (dx, dy) displacement at integer position (x, y).
// For global fields, the same displacement is returned regardless of position.
// For per-pixel fields, out-of-bounds coordinates are clamped to the nearest edge.
func (mf *motionField) displacementAt(x, y int) (float64, float64) {
	if mf.isGlobal {
		return mf.dx, mf.dy
	}

	h := len(mf.fieldX)
	if h == 0 {
		return 0, 0
	}
	w := len(mf.fieldX[0])
	if w == 0 {
		return 0, 0
	}

	cx := clampInt(x, 0, w-1)
	cy := clampInt(y, 0, h-1)
	return mf.fieldX[cy][cx], mf.fieldY[cy][cx]
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
