package kernel

import "fmt"

type ErrNotImplemented struct{}

func (e ErrNotImplemented) Error() string {
	return "not implemented"
}

type ErrUnableToSetSendBufferSize struct {
	Size uint
	Err  error
}

func (e ErrUnableToSetSendBufferSize) Error() string {
	return fmt.Sprintf("unable to set send buffer size to %d: %v", e.Size, e.Err)
}
