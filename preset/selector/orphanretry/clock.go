package orphanretry

import "time"

// NowFunc returns the current time for retry decisions.
type NowFunc func() time.Time
