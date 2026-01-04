// errors.go provides error types for raw network operations.

// Package raw provides raw network operations.
package raw

import "fmt"

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	if e.Err == nil {
		return "not implemented"
	}
	return fmt.Sprintf("not implemented: %v", e.Err)
}

func (e ErrNotImplemented) Unwrap() error {
	return e.Err
}
