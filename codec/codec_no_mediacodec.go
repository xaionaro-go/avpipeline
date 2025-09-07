//go:build !mediacodec
// +build !mediacodec

package codec

import (
	"context"
	"fmt"
)

func (c *Codec) FFAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	return fmt.Errorf("built without MediaCodec support")
}

func (c *Codec) ffAMediaFormatSetInt32(
	ctx context.Context,
	key string,
	value int32,
) (_err error) {
	return fmt.Errorf("built without MediaCodec support")
}
