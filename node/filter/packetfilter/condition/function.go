// function.go implements a condition based on a custom function for packet input filtering.

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
