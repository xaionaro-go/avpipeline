package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
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
	pkt packet.Input,
) bool {
	return !n.Condition.Match(ctx, pkt)
}
