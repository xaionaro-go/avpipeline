// not.go implements a logical NOT condition for packet filtering.

package condition

import (
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type Not = conditionbase.Not[Input]
