// latencies.go defines structures for tracking latencies of different stream types.

package types

import (
	"time"
)

type Latencies struct {
	Audio TrackLatencies
	Video TrackLatencies
}

type TrackLatencies struct {
	PreTranscoding    time.Duration
	Transcoding       time.Duration
	TranscodedPreSend time.Duration
	Sending           time.Duration
}
