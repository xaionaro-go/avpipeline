//go:build with_cv
// +build with_cv

// pixelate.go implements a pixelation (mosaic) image processor for detected regions.

package haarcascadeprocessor

import (
	"context"
	"fmt"
	"image"

	"go.uber.org/atomic"
	"gocv.io/x/gocv"
)

// Pixelate applies a mosaic/pixelation effect to detected regions by
// downscaling then upscaling, creating blocky squares.
type Pixelate struct {
	// BlockSize controls the pixelation block size in pixels.
	// Larger values produce more aggressive pixelation.
	BlockSize atomic.Int64
}

var _ Abstract = (*Pixelate)(nil)

func NewPixelate(blockSize int) *Pixelate {
	p := &Pixelate{}
	p.BlockSize.Store(int64(blockSize))
	return p
}

func (p *Pixelate) String() string {
	return fmt.Sprintf("Pixelate(%d)", p.BlockSize.Load())
}

func (p *Pixelate) Process(
	_ context.Context,
	mat *gocv.Mat,
	coords []image.Rectangle,
) error {
	blockSize := int(p.BlockSize.Load())
	if blockSize <= 1 {
		return nil
	}

	matBounds := image.Rect(0, 0, mat.Cols(), mat.Rows())

	for _, rect := range coords {
		rect = rect.Intersect(matBounds)
		if rect.Empty() {
			continue
		}
		region := mat.Region(rect)
		w := rect.Dx()
		h := rect.Dy()
		smallW := max(1, w/blockSize)
		smallH := max(1, h/blockSize)

		small := gocv.NewMat()
		gocv.Resize(region, &small, image.Pt(smallW, smallH), 0, 0, gocv.InterpolationNearestNeighbor)
		gocv.Resize(small, &region, image.Pt(w, h), 0, 0, gocv.InterpolationNearestNeighbor)
		small.Close()
		region.Close()
	}
	return nil
}
