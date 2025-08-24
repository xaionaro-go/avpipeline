package condition

import (
	"github.com/xaionaro-go/avpipeline/codec/resourcegetter"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[resourcegetter.Input]
