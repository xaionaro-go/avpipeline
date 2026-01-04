//go:build with_libsrt
// +build with_libsrt

// input_srt.go provides SRT-specific functionality for the Input kernel.

package kernel

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (i *Input) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, i.FormatContext)
}
