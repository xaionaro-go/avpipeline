//go:build with_libsrt
// +build with_libsrt

// output_srt.go provides SRT-specific functionality for the Output kernel.

package kernel

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

type GetSRTer interface {
	SRT(ctx context.Context) (*threadsafe.Socket, error)
}

var _ GetSRTer = (*Output)(nil)

func (o *Output) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, o.FormatContext)
}
