package fanin

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

	return c.retryTracker.Tick(ctx, func(
		ctx context.Context,
		routeID id.RouteID,
	) (id.MemberID, bool) {
		if routeID != c.routeID {
			return 0, false
		}
		return c.pair.Current(ctx), true
	})
}
