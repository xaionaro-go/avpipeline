// pts.go defines constants related to Presentation Time Stamps (PTS).

package types

import (
	"math"
)

const (
	PTSKeep = int64(math.MinInt64 + 1)
)
