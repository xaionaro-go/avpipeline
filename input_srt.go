//go:build libsrt
// +build libsrt

package avpipeline

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (input *Input) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, input.FormatContext)
}
