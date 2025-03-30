package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type Function[K types.Abstract] func(context.Context, K) bool

var _ Condition[types.Abstract] = (Function[types.Abstract])(nil)

func (fn Function[K]) String() string {
	var zeroValue K
	return fmt.Sprintf("<custom_function:%p[%T]>", fn, zeroValue)
}

func (fn Function[K]) Match(ctx context.Context, kernel K) bool {
	return fn(ctx, kernel)
}
