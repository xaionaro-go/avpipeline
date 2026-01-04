// Package scaler provides video scaling capabilities using libav.
//
// scaler.go defines the Scaler interface for video scaling.
package scaler

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
)

type Scaler interface {
	fmt.Stringer
	Close(context.Context) error
	ScaleFrame(ctx context.Context, src *astiav.Frame, dst *astiav.Frame) error
	SourceResolution() codec.Resolution
	SourcePixelFormat() astiav.PixelFormat
	DestinationResolution() codec.Resolution
	DestinationPixelFormat() astiav.PixelFormat
}
