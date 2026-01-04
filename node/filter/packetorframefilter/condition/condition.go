// condition.go defines the condition interface for packet-or-frame filters.

// Package condition provides conditions for packet-or-frame filters.
package condition

import (
	"github.com/xaionaro-go/avpipeline/node/filter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type (
	Input     = filter.Input[packetorframe.InputUnion]
	Condition = filter.Condition[packetorframe.InputUnion]
)
