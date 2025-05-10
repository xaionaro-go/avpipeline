package node

import "fmt"

type Error struct {
	Node Abstract
	Err  error
}

func (e Error) Error() string {
	return fmt.Sprintf("received an error on %s: %v", e.Node.GetProcessor(), e.Err)
}

func (e Error) Unwrap() error {
	return e.Err
}

type ErrAlreadyStarted struct{}

func (ErrAlreadyStarted) Error() string {
	return "already started serving"
}
