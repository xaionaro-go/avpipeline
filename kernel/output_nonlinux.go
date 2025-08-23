//go:build !linux
// +build !linux

package kernel

import (
	"context"
)

func (r *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) *uint64 {
	return nil
}
