package fanout

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

type RoutePlanner[K comparable] struct {
	mode         Mode
	routeID      id.RouteID
	splitPlanner PreferredRoutePlanner[K]
}

func NewRoutePlanner[K comparable](
	mode Mode,
	routeID id.RouteID,
	splitPlanner ...PreferredRoutePlanner[K],
) *RoutePlanner[K] {
	var planner PreferredRoutePlanner[K]
	if len(splitPlanner) > 0 {
		planner = splitPlanner[0]
	}

	return &RoutePlanner[K]{
		mode:         mode,
		routeID:      routeID,
		splitPlanner: planner,
	}
}

func (p *RoutePlanner[K]) PlanPreferred(
	ctx context.Context,
	requested K,
) ([]RoutePlan[K], error) {
	switch p.mode {
	case ModeDifferentOutputsSameTracksSplitAV:
		return p.planSplit(ctx, requested)
	default:
		return p.planSingle(requested)
	}
}

func (p *RoutePlanner[K]) planSingle(
	requested K,
) ([]RoutePlan[K], error) {
	plans := []RoutePlan[K]{
		{RouteID: p.routeID, StorageKey: requested},
	}
	if err := validateRoutePlansSyntax(plans); err != nil {
		return nil, err
	}

	return plans, nil
}

func (p *RoutePlanner[K]) planSplit(
	ctx context.Context,
	requested K,
) ([]RoutePlan[K], error) {
	if p.splitPlanner == nil {
		return nil, selectorerr.InvalidRoutePlan("", fmt.Errorf("split planner: %w", ErrMissingRoutePlan))
	}

	plans, err := p.splitPlanner.PlanPreferred(ctx, requested)
	if err != nil {
		return nil, selectorerr.InvalidRoutePlan("", err)
	}
	if err := validateRoutePlansSyntax(plans); err != nil {
		return nil, err
	}

	return plans, nil
}
