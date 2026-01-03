//go:build !mediacodec
// +build !mediacodec

// decoder_no_mediacodec.go provides stub methods for the decoder when MediaCodec is not available.

package codec

import (
	"context"
	"fmt"
)

func (e *DecoderLocked) setLowLatencyMediacodec(
	ctx context.Context,
	v bool,
) error {
	return fmt.Errorf("compiled without mediacodec support")
}
