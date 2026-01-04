//go:build !unix
// +build !unix

// sock_other.go provides fallback socket option implementations for non-Unix systems.

package kernel

func sockSetLinger(
	fd int,
	onOff int32,
	linger int32,
) error {
	return ErrNotImplemented{}
}
