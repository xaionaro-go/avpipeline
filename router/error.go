// error.go defines router-specific error types.

package router

type ErrAlreadyClosed struct{}

func (ErrAlreadyClosed) Error() string  { return "is already closed" }
func (ErrAlreadyClosed) Unwrap() error  { return nil }

type ErrAlreadyOpen struct{}

func (ErrAlreadyOpen) Error() string  { return "is already open" }
func (ErrAlreadyOpen) Unwrap() error  { return nil }

type ErrRouteClosed struct{}

func (ErrRouteClosed) Error() string  { return "the route is closed" }
func (ErrRouteClosed) Unwrap() error  { return nil }

type ErrAlreadyHasPublisher struct{}

func (ErrAlreadyHasPublisher) Error() string  { return "already has a publisher" }
func (ErrAlreadyHasPublisher) Unwrap() error  { return nil }

type ErrAlreadyAPublisher struct{}

func (ErrAlreadyAPublisher) Error() string  { return "is already a publisher" }
func (ErrAlreadyAPublisher) Unwrap() error  { return nil }

type ErrPublisherNotFound struct{}

func (ErrPublisherNotFound) Error() string  { return "publisher not found" }
func (ErrPublisherNotFound) Unwrap() error  { return nil }

type ErrAlreadyAConsumer struct{}

func (ErrAlreadyAConsumer) Error() string  { return "is already a consumer" }
func (ErrAlreadyAConsumer) Unwrap() error  { return nil }

type ErrConsumerNotFound struct{}

func (ErrConsumerNotFound) Error() string  { return "consumer not found" }
func (ErrConsumerNotFound) Unwrap() error  { return nil }
