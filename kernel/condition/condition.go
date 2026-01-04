// condition.go defines the Condition interface for kernels.

// Package condition provides conditions for kernels.
package condition

import (
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition[K kerneltypes.Abstract] = types.Condition[K]
