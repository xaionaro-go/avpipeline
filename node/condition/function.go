// function.go implements a condition based on a custom function for node filtering.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/node"
)

type Function func(context.Context, node.Abstract) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, node node.Abstract) bool {
	return fn(ctx, node)
}
