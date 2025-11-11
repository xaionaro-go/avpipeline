//go:build !unix
// +build !unix

package kernel

func sockSetLinger(
	fd int,
	onOff int32,
	linger int32,
) error {
	return ErrNotImplemented{}
}
