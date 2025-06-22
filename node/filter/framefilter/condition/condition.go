package condition

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node/filter"
)

type Input = filter.Input[frame.Input]
type Condition = filter.Condition[frame.Input]
