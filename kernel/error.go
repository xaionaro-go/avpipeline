package kernel

import "fmt"

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("not implemented: %v", e.Err)
	}
	return "not implemented"
}

func (e ErrNotImplemented) Unwrap() error {
	return e.Err
}

type ErrUnableToSetSendBufferSize struct {
	Size uint
	Err  error
}

func (e ErrUnableToSetSendBufferSize) Error() string {
	return fmt.Sprintf("unable to set send buffer size to %d: %v", e.Size, e.Err)
}

func (e ErrUnableToSetSendBufferSize) Unwrap() error {
	return e.Err
}
