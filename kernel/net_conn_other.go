//go:build !linux
// +build !linux

package kernel

import (
	"context"
	"syscall"
)

func getTCPSocketQueueOS(
	ctx context.Context,
	rawConn syscall.RawConn,
) map[string]uint64 {
	return nil
}
