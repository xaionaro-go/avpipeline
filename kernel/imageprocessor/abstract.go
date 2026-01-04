// abstract.go defines the Abstract interface for image processors.

// Package imageprocessor provides image processing kernels.
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
