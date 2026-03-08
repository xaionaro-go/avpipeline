//go:build !windows

package kernel

import (
	"fmt"
	"syscall"
)

func verifySockType(
	rawConn syscall.RawConn,
	protocolName string,
) error {
	var verifyErr error
	err := rawConn.Control(func(fd uintptr) {
		soType, err := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_TYPE)
		if err != nil {
			verifyErr = fmt.Errorf("getsockopt(SO_TYPE) failed: %w", err)
			return
		}

		switch protocolName {
		case "rtmp", "rtmps":
			if soType != syscall.SOCK_STREAM {
				verifyErr = fmt.Errorf("expected SOCK_STREAM for protocol %s, but got %d", protocolName, soType)
			}
		case "srt", "libsrt", "udp":
			if soType != syscall.SOCK_DGRAM {
				verifyErr = fmt.Errorf("expected SOCK_DGRAM for protocol %s, but got %d", protocolName, soType)
			}
		}
	})
	if err != nil {
		return fmt.Errorf("rawConn.Control failed: %w", err)
	}
	return verifyErr
}
