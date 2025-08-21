//go:build with_libsrt
// +build with_libsrt

package kernel

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

type GetSRTer interface {
	SRT(ctx context.Context) (*threadsafe.Socket, error)
}

var _ GetSRTer = (*Output)(nil)

func (output *Output) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, output.FormatContext)
}
