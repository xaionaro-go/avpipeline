// function.go implements a generic condition based on a custom function.

package condition

import (
	"context"
	"fmt"
)

type Function[T any] func(context.Context, T) bool

func (fn Function[T]) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function[T]) Match(ctx context.Context, v T) bool {
	return fn(ctx, v)
}
