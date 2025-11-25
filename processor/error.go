package processor

import "fmt"

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	return fmt.Sprintf("method not implemented: %s", e.Err)
}

func (e ErrNotImplemented) Unwrap() error {
	return e.Err
}
