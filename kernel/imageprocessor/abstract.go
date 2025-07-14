package imageprocessor

import (
	"context"
	"fmt"
	"image"

	"github.com/xaionaro-go/avpipeline/frame"
)

type Abstract interface {
	fmt.Stringer
	Process(context.Context, frame.Output, image.Rectangle) error
}
