package condition

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[packetorframe.InputUnion]
