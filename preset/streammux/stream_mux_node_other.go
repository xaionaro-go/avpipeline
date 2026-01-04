//go:build !unix
// +build !unix

// stream_mux_node_other.go provides platform-specific error handling for non-Unix systems.

package streammux

import (
	"context"
)

func processPlatformSpecificError(
	ctx context.Context,
	err error,
) error {
	return err
}
