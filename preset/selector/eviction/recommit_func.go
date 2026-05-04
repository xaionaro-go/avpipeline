package eviction

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type RecommitFunc[K comparable] func(ctx context.Context, routeID id.RouteID, storageKey K) error
