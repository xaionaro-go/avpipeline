// condition.go defines the Condition interface for filtering frame inputs to nodes.

// Package condition provides various conditions for filtering frame inputs to nodes.
package condition

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node/filter"
)

type (
	Input     = filter.Input[frame.Input]
	Condition = filter.Condition[frame.Input]
)
