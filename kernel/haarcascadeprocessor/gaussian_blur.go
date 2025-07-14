//go:build with_cv
// +build with_cv

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
	ctx context.Context,
	frame *gocv.Mat,
	coords []image.Rectangle,
) error {
	radius := b.Radius.Load()
	if radius == 0 {
		return nil
	}

	return fmt.Errorf("not implemented")
}
