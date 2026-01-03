// getters.go defines specialized getter interfaces for extracting internal state or connections.

package typesnolibav

import (
	"context"
	"net"
	"syscall"
)

type GetInternalQueueSizer interface {
	GetInternalQueueSize(context.Context) map[string]uint64
}

type GetNetConner interface {
	UnsafeWithNetworkConn(ctx context.Context, callback func(context.Context, net.Conn) error) error
}

type GetSyscallRawConner interface {
	UnsafeWithRawNetworkConn(ctx context.Context, callback func(context.Context, syscall.RawConn, string) error) error
}
