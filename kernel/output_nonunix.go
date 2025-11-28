//go:build !unix
// +build !unix

package kernel

import (
	"context"
)

func (r *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) map[string]uint64 {
	return nil
}
