//go:build with_cv
// +build with_cv

// gaussian_blur.go implements a Gaussian blur image processor using Haar cascades.

package haarcascadeprocessor

import (
	"context"
	"fmt"
	"image"

	"go.uber.org/atomic"
	"gocv.io/x/gocv"
)

type GaussianBlur struct {
	Radius atomic.Float64
}

var _ Abstract = (*GaussianBlur)(nil)

func NewGaussianBlur(radius float64) *GaussianBlur {
	b := &GaussianBlur{}
	b.Radius.Store(radius)
	return b
}

func (b *GaussianBlur) String() string {
	return fmt.Sprintf("GaussianBlur(%v)", b.Radius)
}

func (b *GaussianBlur) Process(
	_ context.Context,
	mat *gocv.Mat,
	coords []image.Rectangle,
) error {
	radius := b.Radius.Load()
	if radius == 0 {
		return nil
	}

	ksize := int(radius)*2 + 1
	size := image.Pt(ksize, ksize)
	matBounds := image.Rect(0, 0, mat.Cols(), mat.Rows())

	for _, rect := range coords {
		rect = rect.Intersect(matBounds)
		if rect.Empty() {
			continue
		}
		region := mat.Region(rect)
		gocv.GaussianBlur(region, &region, size, 0, 0, gocv.BorderDefault)
		region.Close()
	}
	return nil
}
