package imageprocessor

import (
	"context"
	"fmt"
	"image"
	"image/draw"

	"github.com/anthonynsimon/bild/blur"
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"go.uber.org/atomic"
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
	frame frame.Output,
	coords image.Rectangle,
) error {
	radius := b.Radius.Load()
	if radius == 0 {
		return nil
	}
	if frame.GetMediaType() != astiav.MediaTypeVideo {
		return nil
	}

	if err := frame.MakeWritable(); err != nil {
		return fmt.Errorf("unable to make the frame writable: %w", err)
	}

	img, err := frame.Data().GuessImageFormat()
	if err != nil {
		return fmt.Errorf("unable to guess the image format: %w", err)
	}

	if err := frame.Data().ToImage(img); err != nil {
		return fmt.Errorf("unable to convert the image into Go's format: %w", err)
	}

	subImg := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}).SubImage(coords)

	blurredImg := blur.Gaussian(subImg, radius)
	draw.Draw(img.(draw.Image), coords, blurredImg, coords.Min, draw.Over)

	if err := frame.Data().FromImage(img); err != nil {
		return fmt.Errorf("unable to set the modified frame: %w", err)
	}

	return fmt.Errorf("not implemented")
}
