package router

type ErrAlreadyClosed struct{}

func (ErrAlreadyClosed) Error() string {
	return "is already closed"
}
