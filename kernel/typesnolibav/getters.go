package typesnolibav

import (
	"context"
)

type GetInternalQueueSizer interface {
	GetInternalQueueSize(context.Context) map[string]uint64
}
