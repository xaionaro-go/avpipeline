// not.go implements a generic logical NOT condition.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Not[T any] []types.Condition[T]

func (n Not[T]) String() string {
	if len(n) == 1 {
		return fmt.Sprintf("Not(%s)", n[0])
	}
	return fmt.Sprintf("Not(%s)", And[T](n))
}

func (n Not[T]) Match(ctx context.Context, v T) bool {
	return !And[T](n).Match(ctx, v)
}
