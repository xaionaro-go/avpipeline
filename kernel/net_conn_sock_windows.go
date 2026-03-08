//go:build windows

package kernel

import "syscall"

func verifySockType(
	_ syscall.RawConn,
	_ string,
) error {
	return nil
}
