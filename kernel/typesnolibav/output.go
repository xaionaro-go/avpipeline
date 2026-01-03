// output.go defines interfaces for inspecting output queue timing and state.

package typesnolibav

import (
	"context"
	"time"
)

type UnsafeGetOldestDTSInTheQueuer interface {
	UnsafeGetOldestDTSInTheQueue(context.Context) (time.Duration, error)
}
