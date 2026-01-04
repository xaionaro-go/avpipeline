// not.go implements a logical NOT condition.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type Not[K types.Abstract] struct {
	Condition Condition[K]
}

var _ Condition[types.Abstract] = (*Not[types.Abstract])(nil)

func (n Not[K]) String() string {
	return fmt.Sprintf("Not(%s)", n.Condition)
}

func (n Not[K]) Match(
	ctx context.Context,
	kernel K,
) bool {
	return !n.Condition.Match(ctx, kernel)
}
