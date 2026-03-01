// condition.go defines the Condition interface for filtering media packets.

// Package condition provides various conditions for filtering media packets.
package condition

import (
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
)

type Input = packet.Input

type Condition = types.Condition[Input]
