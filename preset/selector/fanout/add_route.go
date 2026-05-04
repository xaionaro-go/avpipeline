package fanout

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func (c *Controller[K, M]) AddRoute(
	ctx context.Context,
	routeID id.RouteID,
	pair *switchpair.Pair,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.routes.Add(ctx, route.State{
		ID:   routeID,
		Pair: pair,
	})
}
