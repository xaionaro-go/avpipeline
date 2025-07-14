//go:build with_cv
// +build with_cv

package haarcascadeprocessor

import (
	"context"
	"fmt"
	"image"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/filter"
)

type AVFilter[T filter.Kernel] struct {
	*filter.Filter[T]
}

var _ kernel.HaarCascadeProcessor = (*AVFilter[filter.Kernel])(nil)

func NewAVFilter[T filter.Kernel](
	filter *filter.Filter[T],
) *AVFilter[T] {
	return &AVFilter[T]{
		Filter: filter,
	}
}

func (f *AVFilter[T]) String() string {
	return fmt.Sprintf("AVFilter(%)", f.Filter)
}

func (f *AVFilter[T]) Process(
	ctx context.Context,
	frame frame.Output,
	coords []image.Rectangle,
) error {
	var img image.RGBA
	err := frame.Data().ToImage(&img)
	if err != nil {
		return fmt.Errorf("unable to convert the frame to RGBA")
	}

	/*for _, coords := range coords {
		err := func(coords image.Rectangle) error {
			f.Filter.Kernel.FilterInput()
		}(coords)
		if err != nil {
			return err
		}
	}*/

	return fmt.Errorf("not implemented")
}
