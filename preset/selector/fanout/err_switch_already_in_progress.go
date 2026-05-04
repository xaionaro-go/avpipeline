package fanout

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type ErrSwitchAlreadyInProgress struct {
	RouteID        id.RouteID
	SwitchMemberID id.MemberID
	SyncerMemberID id.MemberID
}

func (e ErrSwitchAlreadyInProgress) Error() string {
	return fmt.Sprintf(
		"switch already in progress on route %q: switch member %d, syncer member %d",
		e.RouteID,
		e.SwitchMemberID,
		e.SyncerMemberID,
	)
}
