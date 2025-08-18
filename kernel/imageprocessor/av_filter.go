package imageprocessor

import (
	"context"
	"fmt"
	"image"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
)

type AVFilter[T avfilter.Kernel] struct {
	*avfilter.AVFilter[T]
}

var _ Abstract = (*AVFilter[avfilter.Kernel])(nil)

func NewAVFilter[T avfilter.Kernel](
	filter *avfilter.AVFilter[T],
) *AVFilter[T] {
	return &AVFilter[T]{
		AVFilter: filter,
	}
}

func (f *AVFilter[T]) String() string {
	return fmt.Sprintf("AVFilter(%s)", f.AVFilter)
}

func (f *AVFilter[T]) Process(
	ctx context.Context,
	frame frame.Output,
	coords image.Rectangle,
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
