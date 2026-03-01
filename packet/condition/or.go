// or.go implements a logical OR condition for packet filtering.

package condition

import (
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type Or = conditionbase.Or[Input]
