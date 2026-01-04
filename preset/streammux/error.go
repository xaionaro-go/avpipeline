// error.go defines error types for the stream muxer.

package streammux

import (
	"fmt"
)

type ErrSwitchAlreadyInProgress struct {
	OutputIDCurrent OutputID
	OutputIDNext    OutputID
}

func (e ErrSwitchAlreadyInProgress) Error() string {
	return fmt.Sprintf("switch already in progress: %d -> %d", e.OutputIDCurrent, e.OutputIDNext)
}

type ErrOutputAlreadyPreferred struct {
	OutputID OutputID
}

func (e ErrOutputAlreadyPreferred) Error() string {
	return fmt.Sprintf("output %v is already preferred", e.OutputID)
}

type ErrOutputsAlreadyPreferred struct {
	OutputIDs []OutputID
}

func (e ErrOutputsAlreadyPreferred) Error() string {
	return fmt.Sprintf("outputs %v is already preferred", e.OutputIDs)
}

type ErrStop struct{}

func (e ErrStop) Error() string {
	return "stopped"
}
