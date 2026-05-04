package selectorerr

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func InvalidRoutePlan(
	routeID id.RouteID,
	err error,
) error {
	if err == nil {
		return fmt.Errorf("%w: route %q", ErrInvalidRoutePlan, routeID)
	}

	return fmt.Errorf("%w: route %q: %w", ErrInvalidRoutePlan, routeID, err)
}
