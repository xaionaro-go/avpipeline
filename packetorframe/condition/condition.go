// condition.go defines the condition interface for packet-or-frame unions.

// Package condition provides conditions for packet-or-frame unions.
package condition

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[packetorframe.InputUnion]
