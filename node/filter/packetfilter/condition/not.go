package condition

import (
	"context"
	"fmt"
)

type Not []Condition

var _ Condition = (*Not)(nil)

func (n Not) String() string {
	if len(n) == 1 {
		return fmt.Sprintf("Not(%s)", n[0])
	}
	return fmt.Sprintf("Not(%s)", And(n))
}

func (n Not) Match(
	ctx context.Context,
	in Input,
) bool {
	return !And(n).Match(ctx, in)
}
