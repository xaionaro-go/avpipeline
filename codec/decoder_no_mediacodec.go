//go:build !mediacodec
// +build !mediacodec

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
