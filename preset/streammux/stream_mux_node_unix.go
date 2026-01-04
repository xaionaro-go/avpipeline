//go:build unix
// +build unix

// stream_mux_node_unix.go provides platform-specific error handling for Unix systems.

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
