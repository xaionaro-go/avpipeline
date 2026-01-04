// errors.go defines error types for the sockinfo package.

package sockinfo

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	if e.Err == nil {
		return "not implemented"
	}
	return "not implemented: " + e.Err.Error()
}

func (e ErrNotImplemented) Unwrap() error {
	return e.Err
}
