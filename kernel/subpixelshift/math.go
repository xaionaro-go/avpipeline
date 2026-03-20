package subpixelshift

import "math"

// bilinearSample performs bilinear interpolation on a 2D plane at fractional
// position (x, y). Out-of-bounds coordinates are clamped to the nearest edge.
func bilinearSample(
	plane [][]float64,
	x, y float64,
) float64 {
	h := len(plane)
	if h == 0 {
		return 0
	}
	w := len(plane[0])
	if w == 0 {
		return 0
	}

	maxX := float64(w - 1)
	maxY := float64(h - 1)
	x = clampFloat(x, 0, maxX)
	y = clampFloat(y, 0, maxY)

	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := min(x0+1, w-1)
	y1 := min(y0+1, h-1)

	fx := x - float64(x0)
	fy := y - float64(y0)

	v00 := plane[y0][x0]
	v10 := plane[y0][x1]
	v01 := plane[y1][x0]
	v11 := plane[y1][x1]

	return v00*(1-fx)*(1-fy) +
		v10*fx*(1-fy) +
		v01*(1-fx)*fy +
		v11*fx*fy
}

// bilinearWeight returns the proximity weight to the nearest pixel center.
// At integer coordinates the weight is 1.0, at the midpoint (0.5, 0.5) between
// four pixels it is 0.25, and on a single-axis edge it is 0.5.
func bilinearWeight(x, y float64) float64 {
	fx := x - math.Floor(x)
	fy := y - math.Floor(y)

	wx := 1.0 - fx
	if fx > 0.5 {
		wx = fx
	}
	wy := 1.0 - fy
	if fy > 0.5 {
		wy = fy
	}

	return wx * wy
}

// parabolicPeakFit performs sub-pixel peak refinement on a 3x3 neighborhood
// using separate 1D parabolic fits along x and y. The returned (dx, dy) offset
// is clamped to [-0.5, 0.5].
func parabolicPeakFit(
	left, center, right float64,
	top, centerV, bottom float64,
) (dx, dy float64) {
	denomX := left + right - 2*center
	if denomX != 0 {
		dx = (left - right) / (2 * denomX)
	}

	denomY := top + bottom - 2*centerV
	if denomY != 0 {
		dy = (top - bottom) / (2 * denomY)
	}

	dx = clampFloat(dx, -0.5, 0.5)
	dy = clampFloat(dy, -0.5, 0.5)
	return dx, dy
}

// fillGaps iteratively fills zero-weight cells in sum/weightSum grids by
// averaging filled 4-connected neighbors. Repeats until no gaps remain or
// no progress is made.
func fillGaps(
	sum, weightSum [][]float64,
	threshold float64,
) {
	h := len(sum)
	if h == 0 {
		return
	}
	w := len(sum[0])

	// dx/dy offsets for 4-connected neighbors.
	offX := [4]int{-1, 1, 0, 0}
	offY := [4]int{0, 0, -1, 1}

	for {
		filled := 0

		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if weightSum[y][x] > threshold {
					continue
				}

				var accSum float64
				var neighbors int
				for d := 0; d < 4; d++ {
					nx := x + offX[d]
					ny := y + offY[d]
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					if weightSum[ny][nx] <= threshold {
						continue
					}
					accSum += sum[ny][nx] / weightSum[ny][nx]
					neighbors++
				}
				if neighbors == 0 {
					continue
				}

				sum[y][x] = accSum / float64(neighbors)
				weightSum[y][x] = 1.0
				filled++
			}
		}

		if filled == 0 {
			break
		}
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
