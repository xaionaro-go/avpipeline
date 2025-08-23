package types

import (
	"context"
)

type GetInternalQueueSizer interface {
	GetInternalQueueSize(context.Context) map[string]uint64
}

type GetKerneler interface {
	GetKernel() Abstract
}
