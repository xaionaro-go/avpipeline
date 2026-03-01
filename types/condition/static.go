// static.go implements a generic condition that always returns a static boolean value.

package condition

import (
	"context"
	"fmt"
)

type Static[T any] bool

func (v Static[T]) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static[T]) Match(context.Context, T) bool {
	return (bool)(v)
}
