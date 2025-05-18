package router

type ErrAlreadyClosed struct{}

func (ErrAlreadyClosed) Error() string {
	return "is already closed"
}

type ErrAlreadyOpen struct{}

func (ErrAlreadyOpen) Error() string {
	return "is already open"
}

type ErrRouteClosed struct{}

func (ErrRouteClosed) Error() string {
	return "the route is closed"
}
