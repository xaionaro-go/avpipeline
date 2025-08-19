package condition

import (
	"context"
	"fmt"

	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type PTSCond struct {
	Condition mathcondition.Condition[int64]
}

var _ Condition = (*PTSCond)(nil)

func PTS(cond mathcondition.Condition[int64]) PTSCond {
	return PTSCond{
		Condition: cond,
	}
}

func (c PTSCond) String() string {
	return fmt.Sprintf("PTS(%s)", c.Condition)
}

func (c PTSCond) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	PTS := in.GetPTS()
	return c.Condition.Match(PTS)
}
