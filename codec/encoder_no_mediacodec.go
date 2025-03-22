//go:build !mediacodec
// +build !mediacodec

package codec

import (
	"context"
	"fmt"
)

func (e *EncoderFull) setQualityMediacodec(
	ctx context.Context,
	q Quality,
) error {
	return fmt.Errorf("compiled without mediacodec support")
}
