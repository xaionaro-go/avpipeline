//go:build !unix
// +build !unix

package raw

import (
	"context"
	"net"
	"os"
)

func TCPConnFromFD(
	ctx context.Context,
	fd int,
) (_ret net.Conn, _file *os.File, _err error) {
	return nil, nil, ErrNotImplemented{}
}

func WithTCPConnFromFD(
	ctx context.Context,
	fd int,
	callback func(net.Conn) error,
) (_err error) {
	return ErrNotImplemented{}
}
