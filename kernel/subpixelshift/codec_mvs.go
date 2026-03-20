package subpixelshift

import (
	astiav "github.com/asticode/go-astiav"
)

// interpolateMVsToField converts block-level codec motion vectors into a
// per-pixel motion field by splatting each block's displacement onto all
// pixels it covers, averaging where multiple blocks overlap.
func interpolateMVsToField(
	mvs []astiav.MotionVector,
	width, height int,
) motionField {
	if len(mvs) == 0 {
		return zeroMotionField()
	}

	size := height * width
	fieldX := make([]float64, size)
	fieldY := make([]float64, size)
	counts := make([]float64, size)

	for _, mv := range mvs {
		dx := float64(mv.MotionX) / float64(mv.MotionScale)
		dy := float64(mv.MotionY) / float64(mv.MotionScale)

		bw := int(mv.W)
		bh := int(mv.H)
		topX := int(mv.DstX) - bw/2
		topY := int(mv.DstY) - bh/2

		for py := topY; py < topY+bh; py++ {
			if py < 0 || py >= height {
				continue
			}
			rowOff := py * width
			for px := topX; px < topX+bw; px++ {
				if px < 0 || px >= width {
					continue
				}
				idx := rowOff + px
				fieldX[idx] += dx
				fieldY[idx] += dy
				counts[idx]++
			}
		}
	}

	// Average overlapping contributions and reshape into 2D slices.
	fx2d := make([][]float64, height)
	fy2d := make([][]float64, height)
	for y := 0; y < height; y++ {
		rowX := make([]float64, width)
		rowY := make([]float64, width)
		rowOff := y * width
		for x := 0; x < width; x++ {
			idx := rowOff + x
			if counts[idx] > 0 {
				rowX[x] = fieldX[idx] / counts[idx]
				rowY[x] = fieldY[idx] / counts[idx]
			}
		}
		fx2d[y] = rowX
		fy2d[y] = rowY
	}

	return newPerPixelMotionField(fx2d, fy2d, 0.9)
}

// extractCodecMotionField extracts motion vectors from the frame's side data
// and converts them to a per-pixel motion field. Returns false if no motion
// vector side data is present.
func extractCodecMotionField(
	f *astiav.Frame,
) (motionField, bool) {
	if f == nil {
		return zeroMotionField(), false
	}

	mvs, ok := f.SideData().MotionVectors().Get()
	if !ok || len(mvs) == 0 {
		return zeroMotionField(), false
	}

	return interpolateMVsToField(mvs, f.Width(), f.Height()), true
}
