// or.go implements a logical OR condition for packet-or-frame filters.

package condition

import (
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type Or = conditionbase.Or[Input]
