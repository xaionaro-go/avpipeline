package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
)

type Not []Condition

var _ Condition = (*Not)(nil)

func (n Not) String() string {
	return fmt.Sprintf("!%s", And(n))
}

func (n Not) Match(
	ctx context.Context,
	f frame.Input,
) bool {
	return !And(n).Match(ctx, f)
}
