// condition.go defines the Condition interface for filtering media frames.

// Package condition provides various conditions for filtering media frames.
package condition

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[frame.Input]
