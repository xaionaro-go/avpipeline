// error.go contains common error types for the typesnolibav package.

package typesnolibav

type ErrNotImplemented struct{}

func (ErrNotImplemented) Error() string {
	return "not implemented"
}

type ErrUnexpectedInputType struct{}

func (ErrUnexpectedInputType) Error() string {
	return "unexpected input type"
}
