package types

import (
	"context"
)

type GetInternalQueueSizer interface {
	GetInternalQueueSize(context.Context) *uint64
}
