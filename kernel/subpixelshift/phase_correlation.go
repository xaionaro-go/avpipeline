package subpixelshift

import (
	"fmt"
	"math"
	"math/cmplx"
)

const minPhaseCorrelationSize = 8

// hannWindow returns a 2D Hann (raised cosine) window of size w x h.
// Corners are near 0, center is near 1. Used to reduce spectral leakage
// before FFT.
func hannWindow(w, h int) [][]float64 {
	win := make([][]float64, h)
	for y := range win {
		row := make([]float64, w)
		wy := 0.5 * (1 - math.Cos(2*math.Pi*float64(y)/float64(h)))
		for x := range row {
			wx := 0.5 * (1 - math.Cos(2*math.Pi*float64(x)/float64(w)))
			row[x] = wx * wy
		}
		win[y] = row
	}
	return win
}

// nextPow2 returns the smallest power of 2 that is >= n.
func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// fft performs an in-place Cooley-Tukey radix-2 FFT.
// The input length must be a power of 2.
func fft(data []complex128) {
	n := len(data)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation.
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			data[i], data[j] = data[j], data[i]
		}
	}

	// Butterfly stages.
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		wn := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := 0; k < half; k++ {
				u := data[start+k]
				v := w * data[start+k+half]
				data[start+k] = u + v
				data[start+k+half] = u - v
				w *= wn
			}
		}
	}
}

// ifft performs an in-place inverse FFT by conjugating, applying the forward
// FFT, conjugating again, and scaling by 1/N.
func ifft(data []complex128) {
	n := len(data)
	for i := range data {
		data[i] = cmplx.Conj(data[i])
	}

	fft(data)

	scale := complex(1.0/float64(n), 0)
	for i := range data {
		data[i] = cmplx.Conj(data[i]) * scale
	}
}

// fft2D performs a 2D FFT by applying 1D FFT to each row, then each column.
func fft2D(plane [][]complex128) {
	h := len(plane)
	if h == 0 {
		return
	}
	w := len(plane[0])

	// FFT each row.
	for y := 0; y < h; y++ {
		fft(plane[y])
	}

	// FFT each column.
	col := make([]complex128, h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = plane[y][x]
		}
		fft(col)
		for y := 0; y < h; y++ {
			plane[y][x] = col[y]
		}
	}
}

// ifft2D performs a 2D inverse FFT by applying 1D IFFT to each row, then each column.
func ifft2D(plane [][]complex128) {
	h := len(plane)
	if h == 0 {
		return
	}
	w := len(plane[0])

	// IFFT each row.
	for y := 0; y < h; y++ {
		ifft(plane[y])
	}

	// IFFT each column.
	col := make([]complex128, h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = plane[y][x]
		}
		ifft(col)
		for y := 0; y < h; y++ {
			plane[y][x] = col[y]
		}
	}
}

// estimateGlobalMotion uses FFT-based phase correlation to estimate the global
// sub-pixel displacement between a reference frame and a current frame.
//
// Both frames are provided as 2D float64 planes (e.g. luma channel).
// Returns a global motionField with (dx, dy) displacement and a confidence
// score derived from the sharpness of the correlation peak.
func estimateGlobalMotion(
	reference, current [][]float64,
) (motionField, error) {
	refH := len(reference)
	if refH == 0 {
		return zeroMotionField(), fmt.Errorf("reference frame is empty")
	}
	refW := len(reference[0])

	curH := len(current)
	if curH == 0 {
		return zeroMotionField(), fmt.Errorf("current frame is empty")
	}
	curW := len(current[0])

	if refW < minPhaseCorrelationSize || refH < minPhaseCorrelationSize {
		return zeroMotionField(), fmt.Errorf(
			"reference frame too small: %dx%d, minimum %dx%d",
			refW, refH, minPhaseCorrelationSize, minPhaseCorrelationSize,
		)
	}
	if curW < minPhaseCorrelationSize || curH < minPhaseCorrelationSize {
		return zeroMotionField(), fmt.Errorf(
			"current frame too small: %dx%d, minimum %dx%d",
			curW, curH, minPhaseCorrelationSize, minPhaseCorrelationSize,
		)
	}

	// Use the common area of both frames.
	w := min(refW, curW)
	h := min(refH, curH)

	// Pad to a square power-of-2 size for the FFT.
	n := nextPow2(max(w, h))

	win := hannWindow(w, h)

	f1 := zeroPadAndWindow(reference, w, h, n, win)
	f2 := zeroPadAndWindow(current, w, h, n, win)

	fft2D(f1)
	fft2D(f2)

	// Normalized cross-power spectrum.
	R := crossPowerSpectrum(f1, f2, n)

	ifft2D(R)

	peakX, peakY, peakVal, meanVal := findPeak(R, n)

	// Wrap-around: if peak index > N/2, the shift is negative.
	dx := peakX
	if dx > n/2 {
		dx -= n
	}
	dy := peakY
	if dy > n/2 {
		dy -= n
	}

	// Sub-pixel refinement via parabolic fit on the 3x3 neighborhood.
	subDX, subDY := subPixelRefine(R, n, peakX, peakY)

	finalDX := float64(dx) + subDX
	finalDY := float64(dy) + subDY

	// Confidence = sharpness ratio (peak / mean absolute value).
	// A sharp peak relative to the background indicates reliable estimation.
	confidence := 0.0
	absMean := math.Abs(meanVal)
	if absMean > 1e-15 {
		confidence = peakVal / absMean
	}

	return newGlobalMotionField(finalDX, finalDY, confidence), nil
}

// zeroPadAndWindow copies the source plane into an NxN complex plane,
// applying the Hann window and zero-padding the remainder.
func zeroPadAndWindow(
	src [][]float64,
	w, h, n int,
	win [][]float64,
) [][]complex128 {
	plane := make([][]complex128, n)
	for y := 0; y < n; y++ {
		row := make([]complex128, n)
		if y < h {
			for x := 0; x < w; x++ {
				row[x] = complex(src[y][x]*win[y][x], 0)
			}
		}
		plane[y] = row
	}
	return plane
}

// crossPowerSpectrum computes the normalized cross-power spectrum:
// R[i][j] = F2[i][j] * conj(F1[i][j]) / |F2[i][j] * conj(F1[i][j])|
//
// Using F_current * conj(F_reference) so that the IFFT peaks at the
// displacement of current relative to reference (positive = content moved
// in the positive direction).
func crossPowerSpectrum(
	f1, f2 [][]complex128,
	n int,
) [][]complex128 {
	R := make([][]complex128, n)
	for y := 0; y < n; y++ {
		row := make([]complex128, n)
		for x := 0; x < n; x++ {
			product := f2[y][x] * cmplx.Conj(f1[y][x])
			mag := cmplx.Abs(product)
			if mag > 1e-15 {
				row[x] = product / complex(mag, 0)
			}
		}
		R[y] = row
	}
	return R
}

// findPeak locates the integer peak (maximum real value) in the correlation
// surface and computes the mean of all real values.
func findPeak(
	R [][]complex128,
	n int,
) (peakX, peakY int, peakVal, meanVal float64) {
	peakVal = math.Inf(-1)
	sum := 0.0

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := real(R[y][x])
			sum += v
			if v > peakVal {
				peakVal = v
				peakX = x
				peakY = y
			}
		}
	}

	total := float64(n * n)
	if total > 0 {
		meanVal = sum / total
	}
	return peakX, peakY, peakVal, meanVal
}

// subPixelRefine applies parabolicPeakFit on the 3x3 neighborhood around
// the integer peak, using wrap-around indexing for the correlation surface.
func subPixelRefine(
	R [][]complex128,
	n int,
	peakX, peakY int,
) (dx, dy float64) {
	at := func(x, y int) float64 {
		// Wrap-around indexing for the circular correlation surface.
		return real(R[((y%n)+n)%n][((x%n)+n)%n])
	}

	center := at(peakX, peakY)
	left := at(peakX-1, peakY)
	right := at(peakX+1, peakY)
	top := at(peakX, peakY-1)
	bottom := at(peakX, peakY+1)

	return parabolicPeakFit(left, center, right, top, center, bottom)
}
