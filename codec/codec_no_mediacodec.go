//go:build !mediacodec
// +build !mediacodec

// codec_no_mediacodec.go provides stub methods for Codec when MediaCodec is not available.

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
