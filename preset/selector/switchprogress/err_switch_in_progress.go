package switchprogress

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type ErrSwitchInProgress struct {
	ProcN int64
	To    id.MemberID
}

func (e ErrSwitchInProgress) Error() string {
	return fmt.Sprintf("another switch is in progress (procN: %d), cannot switch to %d", e.ProcN, e.To)
}

func (e ErrSwitchInProgress) Is(target error) bool {
	_, ok := target.(ErrSwitchInProgress)
	return ok
}
