//go:build with_libsrt
// +build with_libsrt

package kernel

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (i *Input) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, i.FormatContext)
}
