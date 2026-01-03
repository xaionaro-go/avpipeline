// Package raw provides utilities for working with raw network connections.
package raw

import (
	"fmt"
	"net"
	"os"
	"syscall"

	tcpopt "github.com/xaionaro-go/tcp/opt"
	tcpsyscall "github.com/xaionaro-go/tcp/syscall"
)

func WithTCPConnFromFD(
	fd int,
	callback func(net.Conn) error,
) error {
	f := os.NewFile(uintptr(fd), "socketfd")
	defer f.Close()

	conn, err := net.FileConn(f)
	if err != nil {
		return fmt.Errorf("unable to build net.Conn from file descriptor %d: %w", fd, err)
	}
	defer conn.Close()

	err = callback(conn)
	if err != nil {
		return fmt.Errorf("callback failed: %w", err)
	}

	return nil
}

func SetOption(
	rawConn syscall.RawConn,
	opt tcpopt.Option,
) error {
	b, err := opt.Marshal()
	if err != nil {
		return fmt.Errorf("unable to marshal option: %w", err)
	}
	var setErr error
	err = rawConn.Control(func(fd uintptr) {
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
	rawConn syscall.RawConn,
	opt tcpopt.Option,
) (tcpopt.Option, error) {
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
