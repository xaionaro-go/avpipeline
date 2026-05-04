package orphanretry

import "time"

// Decision is the retry policy decision for one orphaned route.
type Decision struct {
	Attempt bool
	Retire  bool
	NextAt  time.Time
}
