//go:build unix
// +build unix

package raw

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/xaionaro-go/avpipeline/logger"
	"golang.org/x/sys/unix"
)

func ConnFromFD(
	ctx context.Context,
	fd int,
) (_ret net.Conn, _file *os.File, _err error) {
	logger.Tracef(ctx, "ConnFromFD: %d", fd)
	defer func() { logger.Tracef(ctx, "/ConnFromFD: %d: %v", fd, _err) }()

	fdDup, err := unix.Dup(fd)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to dup file descriptor %d: %w", fd, err)
	}

	f := os.NewFile(uintptr(fdDup), "socketfd")

	conn, err := net.FileConn(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("unable to build net.Conn from file descriptor %d (dup:%d): %w", fd, f.Fd(), err)
	}

	return conn, f, nil
}

func WithConnFromFD(
	ctx context.Context,
	fd int,
	callback func(net.Conn) error,
) (_err error) {
	logger.Tracef(ctx, "WithConnFromFD: %d", fd)
	defer func() { logger.Tracef(ctx, "/WithConnFromFD: %d: %v", fd, _err) }()

	conn, f, err := ConnFromFD(ctx, fd)
	if err != nil {
		return err
	}
	defer f.Close()

	err = callback(conn)
	if err != nil {
		return fmt.Errorf("callback failed: %w", err)
	}

	return nil
}
