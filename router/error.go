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

type ErrAlreadyHasPublisher struct{}

func (ErrAlreadyHasPublisher) Error() string {
	return "is already has a publisher"
}

type ErrAlreadyAPublisher struct{}

func (ErrAlreadyAPublisher) Error() string {
	return "is already a publisher"
}

type ErrPublisherNotFound struct{}

func (ErrPublisherNotFound) Error() string {
	return "publisher not found"
}
