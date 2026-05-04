package selectorerr

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func RouteNotFound(routeID id.RouteID) error {
	return fmt.Errorf("%w: route %q", ErrRouteNotFound, routeID)
}
