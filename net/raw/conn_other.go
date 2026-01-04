//go:build !unix
// +build !unix

package raw

import (
	"context"
	"net"
	"os"
)

func ConnFromFD(
	ctx context.Context,
	fd int,
) (_ret net.Conn, _file *os.File, _err error) {
	return nil, nil, ErrNotImplemented{}
}

func WithConnFromFD(
	ctx context.Context,
	fd int,
	callback func(net.Conn) error,
) (_err error) {
	return ErrNotImplemented{}
}
