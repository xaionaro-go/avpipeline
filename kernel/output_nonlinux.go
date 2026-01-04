//go:build !linux
// +build !linux

// output_nonlinux.go provides fallback implementations for the Output kernel on non-Linux systems.

package kernel

import (
	"context"
)

func (r *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) map[string]uint64 {
	return nil
}
