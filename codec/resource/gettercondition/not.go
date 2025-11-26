package gettercondition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/resource"
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
	f resource.GetterInput,
) bool {
	return !n.Condition.Match(ctx, f)
}
