// getters.go defines type aliases for various getter interfaces used in the kernel.

package kernel

import (
	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type (
	GetInternalQueueSizer = types.GetInternalQueueSizer
	GetKerneler           = types.GetKerneler
	GetNetConner          = types.GetNetConner
	GetSyscallRawConner   = types.GetSyscallRawConner
)
