// pts.go defines constants related to Presentation Time Stamps (PTS).

package types

import (
	"math"
)

const (
	// PTSKeep means "do not modify the input PTS".
	PTSKeep = int64(math.MinInt64 + 1)

	// PTSEpoch means "rebase the first packet of every stream onto the
	// shared process-wide monotonic epoch (kernel.PTSEpochNanos). Each
	// stream's first PTS is set to (now - epoch) converted into that
	// stream's own timebase". Intended for live media kernels (camera,
	// microphone, ...) so that A/V tracks opened independently land on
	// a common origin instead of each kernel restarting at PTS=0 with
	// its own cold-start latency baked in.
	PTSEpoch = int64(math.MinInt64 + 2)
)
