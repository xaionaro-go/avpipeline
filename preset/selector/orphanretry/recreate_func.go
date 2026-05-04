package orphanretry

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

// RecreateFunc recreates a member for an orphaned route and storage key.
type RecreateFunc[K comparable] func(ctx context.Context, routeID id.RouteID, storageKey K) error
