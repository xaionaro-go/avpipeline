package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
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
	pkt frame.Input,
) bool {
	return !n.Condition.Match(ctx, pkt)
}
