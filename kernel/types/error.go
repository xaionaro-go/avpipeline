package types

type ErrNotImplemented struct{}

func (ErrNotImplemented) Error() string {
	return "not implemented"
}
