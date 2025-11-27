package condition

import (
	"context"
	"fmt"

	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type DTSCond struct {
	Condition mathcondition.Condition[int64]
}

var _ Condition = (*DTSCond)(nil)

func DTS(cond mathcondition.Condition[int64]) DTSCond {
	return DTSCond{
		Condition: cond,
	}
}

func (c DTSCond) String() string {
	return fmt.Sprintf("DTS(%s)", c.Condition)
}

func (c DTSCond) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	dts := in.GetDTS()
	return c.Condition.Match(ctx, dts)
}
