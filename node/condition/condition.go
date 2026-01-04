// condition.go defines the Condition interface for filtering nodes.

// Package condition provides various conditions for filtering nodes.
package condition

import (
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[node.Abstract]
