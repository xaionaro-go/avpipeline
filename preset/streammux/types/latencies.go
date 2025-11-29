package types

import (
	"time"
)

type Latencies struct {
	Audio TrackLatencies
	Video TrackLatencies
}

type TrackLatencies struct {
	PreRecoding    time.Duration
	Recoding       time.Duration
	RecodedPreSend time.Duration
	Sending        time.Duration
}
