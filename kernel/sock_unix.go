//go:build unix
// +build unix

// sock_unix.go provides Unix-specific socket option implementations.

package kernel

import (
	"golang.org/x/sys/unix"
)

func sockSetLinger(
	fd int,
	onOff int32,
	linger int32,
) error {
	return unix.SetsockoptLinger(fd, unix.SOL_SOCKET, unix.SO_LINGER, &unix.Linger{
		Onoff:  onOff,
		Linger: linger,
	})
}
