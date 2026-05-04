package orphanretry

import "time"

// Policy decides whether an orphaned route should be recreated or retired.
type Policy[K comparable] interface {
	Next(now time.Time, state State[K]) Decision
}
