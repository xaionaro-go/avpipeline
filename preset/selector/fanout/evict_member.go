package fanout

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
)

func (c *Controller[K, M]) EvictMember(
	ctx context.Context,
	dead member.Entry[K, M],
) (eviction.Result, error) {
	return c.eviction.Evict(ctx, dead)
}
