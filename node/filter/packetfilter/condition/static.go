// static.go implements a condition that always returns a static boolean value for packet input filtering.

package condition

import (
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type Static = conditionbase.Static[Input]
