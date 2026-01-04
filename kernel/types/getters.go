// getters.go defines various getter interfaces for kernels.

package types

import (
	"github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
)

type GetKerneler interface {
	GetKernel() Abstract
}

type (
	GetInternalQueueSizer = typesnolibav.GetInternalQueueSizer
	GetNetConner          = typesnolibav.GetNetConner
	GetSyscallRawConner   = typesnolibav.GetSyscallRawConner
)
