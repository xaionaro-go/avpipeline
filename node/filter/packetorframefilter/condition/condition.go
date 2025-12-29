package condition

import (
	"github.com/xaionaro-go/avpipeline/node/filter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Input = filter.Input[packetorframe.InputUnion]
type Condition = filter.Condition[packetorframe.InputUnion]
