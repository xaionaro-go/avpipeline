//go:build libsrt
// +build libsrt

package kernel

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (output *Output) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, output.FormatContext)
}
