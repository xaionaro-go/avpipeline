// output.go defines interfaces for inspecting output queue timing and state.

package typesnolibav

import (
	"context"
	"time"
)

type GetOldestDTSInTheQueuer interface {
	GetOldestDTSInTheQueue(context.Context) (time.Duration, error)
}
