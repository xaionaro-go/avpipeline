package fanout

import "context"

type PreferredRoutePlanner[K comparable] interface {
	PlanPreferred(ctx context.Context, requested K) ([]RoutePlan[K], error)
}

type PreferredRoutePlannerFunc[K comparable] func(
	ctx context.Context,
	requested K,
) ([]RoutePlan[K], error)

func (f PreferredRoutePlannerFunc[K]) PlanPreferred(
	ctx context.Context,
	requested K,
) ([]RoutePlan[K], error) {
	return f(ctx, requested)
}
