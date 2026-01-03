//go:build !mediacodec
// +build !mediacodec

// encoder_full_no_mediacodec.go provides stub methods for the encoder when MediaCodec is not available.

package codec

import (
	"context"
	"fmt"
)

func (e *EncoderFullLocked) setQualityMediacodec(
	ctx context.Context,
	q Quality,
) error {
	return fmt.Errorf("compiled without mediacodec support")
}
