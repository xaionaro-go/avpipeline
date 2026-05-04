package orphanretry

import (
	"time"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

// State is the retry state for one orphaned route.
type State[K comparable] struct {
	RouteID        id.RouteID
	StorageKey     K
	Attempts       uint64
	FirstAttemptAt time.Time
	LastAttemptAt  time.Time
	NextAttemptAt  time.Time
}
