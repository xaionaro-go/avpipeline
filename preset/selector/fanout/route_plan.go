package fanout

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

var (
	ErrMissingRoutePlan     = errors.New("missing route plan")
	ErrMissingRouteID       = errors.New("missing route id")
	ErrDuplicateRouteInPlan = errors.New("duplicate route in plan")
	ErrMemberNotAttached    = errors.New("member is not attached to route")
)

type RoutePlan[K comparable] struct {
	RouteID    id.RouteID
	StorageKey K
}

func validateRouteIDs(
	ctx context.Context,
	routes *route.Registry,
	routeIDs []id.RouteID,
) error {
	if len(routeIDs) == 0 {
		return selectorerr.InvalidRoutePlan("", ErrMissingRoutePlan)
	}

	seen := map[id.RouteID]struct{}{}
	for _, routeID := range routeIDs {
		if routeID == "" {
			return selectorerr.InvalidRoutePlan(routeID, ErrMissingRouteID)
		}
		if _, ok := seen[routeID]; ok {
			return selectorerr.InvalidRoutePlan(routeID, ErrDuplicateRouteInPlan)
		}
		seen[routeID] = struct{}{}

		if _, ok := routes.Load(ctx, routeID); !ok {
			return selectorerr.InvalidRoutePlan(routeID, selectorerr.RouteNotFound(routeID))
		}
	}

	return nil
}

func validateRoutePlansSyntax[K comparable](
	plans []RoutePlan[K],
) error {
	if len(plans) == 0 {
		return selectorerr.InvalidRoutePlan("", ErrMissingRoutePlan)
	}

	seen := map[id.RouteID]struct{}{}
	for _, plan := range plans {
		if plan.RouteID == "" {
			return selectorerr.InvalidRoutePlan(plan.RouteID, ErrMissingRouteID)
		}
		if _, ok := seen[plan.RouteID]; ok {
			return selectorerr.InvalidRoutePlan(plan.RouteID, ErrDuplicateRouteInPlan)
		}
		seen[plan.RouteID] = struct{}{}
	}

	return nil
}

func unattachedRoutePlanError(
	routeID id.RouteID,
	safeKey string,
) error {
	return selectorerr.InvalidRoutePlan(
		routeID,
		fmt.Errorf("storage key %s: %w", safeKey, ErrMemberNotAttached),
	)
}
