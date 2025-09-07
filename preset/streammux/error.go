package streammux

import (
	"fmt"
)

type ErrSwitchAlreadyInProgress struct {
	OutputIDCurrent int32
	OutputIDNext    int32
}

func (e ErrSwitchAlreadyInProgress) Error() string {
	return fmt.Sprintf("switch already in progress: %d -> %d", e.OutputIDCurrent, e.OutputIDNext)
}
