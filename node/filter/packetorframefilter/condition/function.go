// function.go implements a condition that uses a custom function.

package condition

import (
	"context"
	"fmt"
)

type Function func(context.Context, Input) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, in Input) bool {
	return fn(ctx, in)
}
