// Package raw provides utilities for working with raw network connections.
package raw

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/xaionaro-go/avpipeline/logger"
	tcpopt "github.com/xaionaro-go/tcp/opt"
	tcpsyscall "github.com/xaionaro-go/tcp/syscall"
)

func WithTCPConnFromFD(
	ctx context.Context,
	fd int,
	callback func(net.Conn) error,
) (_err error) {
	logger.Tracef(ctx, "WithTCPConnFromFD: %d", fd)
	defer func() { logger.Tracef(ctx, "/WithTCPConnFromFD: %d: %v", fd, _err) }()

	f := os.NewFile(uintptr(fd), "socketfd")
	defer f.Close()

	conn, err := net.FileConn(f)
	if err != nil {
		return fmt.Errorf("unable to build net.Conn from file descriptor %d: %w", fd, err)
	}

	err = callback(conn)
	if err != nil {
		return fmt.Errorf("callback failed: %w", err)
	}

	return nil
}

func SetOption(
	ctx context.Context,
	rawConn syscall.RawConn,
	opt tcpopt.Option,
) (_err error) {
	logger.Debugf(ctx, "SetOption: %#+v", opt)
	defer func() { logger.Debugf(ctx, "/SetOption: %#+v: %v", opt, _err) }()

	b, err := opt.Marshal()
	if err != nil {
		return fmt.Errorf("unable to marshal option: %w", err)
	}
	var setErr error
	err = rawConn.Control(func(fd uintptr) {
		logger.Debugf(ctx, "setting option on fd %d: %#+v", fd, opt)
		setErr = tcpsyscall.Setsockopt(fd, opt.Level(), opt.Name(), b)
	})
	if err != nil {
		return fmt.Errorf("'Control' failure: %w", err)
	}
	if setErr != nil {
		return fmt.Errorf("unable to set option: %w", setErr)
	}
	return nil
}

func GetOption(
	ctx context.Context,
	rawConn syscall.RawConn,
	opt tcpopt.Option,
) (_ret tcpopt.Option, _err error) {
	logger.Tracef(ctx, "GetOption: %#+v", opt)
	defer func() { logger.Tracef(ctx, "/GetOption: %v", _ret, _err) }()

	level, name := opt.Level(), opt.Name()

	var buf [256]byte
	b := buf[:]
	var opErr error
	err := rawConn.Control(func(fd uintptr) {
		ti, err := tcpsyscall.Getsockopt(fd, level, name, b)
		if err != nil {
			opErr = err
			return
		}
		b = b[:ti]
	})
	if err != nil {
		return nil, fmt.Errorf("'Control' failed: %w", err)
	}
	if opErr != nil {
		return nil, fmt.Errorf("'Getsockopt' failed: %w", opErr)
	}

	optParsed, err := tcpopt.Parse(level, name, b)
	if err != nil {
		return nil, fmt.Errorf("parsing option failed: %w", err)
	}

	return optParsed, nil
}
