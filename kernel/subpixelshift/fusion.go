package subpixelshift

import "math"

// hrPlane accumulates weighted pixel contributions for one color plane
// of the high-resolution output.
type hrPlane struct {
	sum       [][]float64
	weightSum [][]float64
}

// hrGrid holds the accumulation buffers for all planes of the HR output.
type hrGrid struct {
	width  int
	height int
	planes []hrPlane
}

// newHRGrid allocates a grid with contiguous backing memory for each plane.
func newHRGrid(
	width, height, numPlanes int,
) *hrGrid {
	planes := make([]hrPlane, numPlanes)
	for i := range planes {
		planes[i] = hrPlane{
			sum:       make2D(height, width),
			weightSum: make2D(height, width),
		}
	}
	return &hrGrid{
		width:  width,
		height: height,
		planes: planes,
	}
}

// reset zeroes all accumulation buffers across all planes.
func (g *hrGrid) reset() {
	for i := range g.planes {
		zero2D(g.planes[i].sum)
		zero2D(g.planes[i].weightSum)
	}
}

// make2D allocates a rows x cols 2D slice backed by a single contiguous
// allocation to improve cache locality.
func make2D(rows, cols int) [][]float64 {
	data := make([]float64, rows*cols)
	m := make([][]float64, rows)
	for i := range m {
		m[i] = data[i*cols : (i+1)*cols]
	}
	return m
}

// zero2D zeroes all elements in a 2D slice.
func zero2D(m [][]float64) {
	for _, row := range m {
		clear(row)
	}
}

// temporalWeight returns an exponential decay weight based on frame age.
// Age 0 yields 1.0, and the weight is symmetric (age -1 == age +1).
func temporalWeight(age int) float64 {
	return math.Exp(-0.3 * math.Abs(float64(age)))
}

// isExcessiveMotion returns true if the motion field indicates unreliable or
// excessively large motion that should be rejected during fusion.
func isExcessiveMotion(
	mf motionField,
	frameWidth, frameHeight int,
) bool {
	if mf.confidence < 0.2 {
		return true
	}

	if !mf.isGlobal {
		return false
	}

	// For global motion, reject if displacement exceeds 25% of frame size.
	maxDisp := 0.25 * math.Max(float64(frameWidth), float64(frameHeight))
	magnitude := math.Hypot(mf.dx, mf.dy)
	return magnitude > maxDisp
}

// fuseFrame accumulates one LR frame's contribution into a single HR plane.
// Each HR pixel is mapped back to LR coordinates via the inverse of the
// upscaling and motion displacement, then bilinearly sampled and weighted.
func fuseFrame(
	grid *hrGrid,
	planeIdx int,
	lrPlane [][]float64,
	mf motionField,
	scale int,
	age int,
	confidence float64,
) {
	hp := &grid.planes[planeIdx]
	tw := temporalWeight(age)
	scaleF := float64(scale)

	lrH := len(lrPlane)
	if lrH == 0 {
		return
	}
	lrW := len(lrPlane[0])

	for hy := 0; hy < grid.height; hy++ {
		for hx := 0; hx < grid.width; hx++ {
			// Map HR pixel back to LR coordinates.
			lrBaseX := float64(hx) / scaleF
			lrBaseY := float64(hy) / scaleF

			// Get displacement at the nearest integer LR position.
			nearX := clampInt(int(math.Round(lrBaseX)), 0, lrW-1)
			nearY := clampInt(int(math.Round(lrBaseY)), 0, lrH-1)
			mdx, mdy := mf.displacementAt(nearX, nearY)

			// Subtract motion to compensate: where was this pixel in the LR frame?
			lx := lrBaseX - mdx
			ly := lrBaseY - mdy

			// Skip if outside LR bounds.
			if lx < 0 || lx > float64(lrW-1) || ly < 0 || ly > float64(lrH-1) {
				continue
			}

			sample := bilinearSample(lrPlane, lx, ly)
			weight := tw * bilinearWeight(lx, ly) * confidence

			hp.sum[hy][hx] += sample * weight
			hp.weightSum[hy][hx] += weight
		}
	}
}

// extractHRPlane normalizes the accumulated HR plane and fills any remaining
// gaps via neighbor interpolation.
func extractHRPlane(
	hp *hrPlane,
	width, height int,
	gapThreshold float64,
) [][]float64 {
	result := make2D(height, width)
	ws := make2D(height, width)

	// Normalize where weight is above threshold.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ws[y][x] = hp.weightSum[y][x]
			if ws[y][x] >= gapThreshold {
				result[y][x] = hp.sum[y][x] / ws[y][x]
			}
		}
	}

	// fillGaps averages filled 4-connected neighbors into gap cells,
	// storing the interpolated value directly in result and setting
	// ws to 1.0 for newly filled cells.
	fillGaps(result, ws, gapThreshold)

	return result
}
