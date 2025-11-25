//go:build unix
// +build unix

package streammux

import (
	"context"
	"errors"

	"golang.org/x/sys/unix"
)

func processPlatformSpecificError(
	ctx context.Context,
	err error,
) error {
	if errors.Is(err, unix.EPIPE) {
		return nil
	}
	return err
}
