//go:build !linux
// +build !linux

package kernel

import (
	"context"
)

func (r *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) map[string]uint64 {
	return nil
}
