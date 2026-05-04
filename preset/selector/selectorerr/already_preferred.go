package selectorerr

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func AlreadyPreferred(
	routeID id.RouteID,
	memberID id.MemberID,
) error {
	return fmt.Errorf("%w: route %q member %d", ErrAlreadyPreferred, routeID, memberID)
}
