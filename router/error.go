package router

type ErrAlreadyClosed struct{}

func (ErrAlreadyClosed) Error() string {
	return "is already closed"
}

type ErrRouteClosed struct{}

func (ErrRouteClosed) Error() string {
	return "the route is closed"
}
