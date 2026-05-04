package availability

import "context"

// Source reports whether a present selector candidate currently has resources.
type Source interface {
	HasResources(ctx context.Context) bool
}
