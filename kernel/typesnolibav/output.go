package typesnolibav

import (
	"context"
	"time"
)

type UnsafeGetOldestDTSInTheQueuer interface {
	UnsafeGetOldestDTSInTheQueue(context.Context) (time.Duration, error)
}
