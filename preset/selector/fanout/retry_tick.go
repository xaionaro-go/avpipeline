package fanout

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func (c *Controller[K, M]) RetryTick(
	ctx context.Context,
) error {
	if c.retryTracker == nil {
		return nil
	}

	return c.retryTracker.Tick(ctx, c.currentRouteMember)
}

func (c *Controller[K, M]) currentRouteMember(
	ctx context.Context,
	routeID id.RouteID,
) (id.MemberID, bool) {
	state, ok := c.routes.Load(ctx, routeID)
	if !ok || state.Pair == nil {
		return 0, false
	}

	return state.Pair.Current(ctx), true
}
