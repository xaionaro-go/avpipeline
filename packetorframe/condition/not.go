package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Not struct {
	Condition Condition
}

var _ Condition = (*Not)(nil)

func (n Not) String() string {
	return fmt.Sprintf("Not(%s)", n.Condition)
}

func (n Not) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	return !n.Condition.Match(ctx, in)
}
