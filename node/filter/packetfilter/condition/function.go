// function.go implements a condition based on a custom function for packet input filtering.

package condition

import (
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type Function = conditionbase.Function[Input]
