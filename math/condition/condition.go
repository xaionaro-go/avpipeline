package condition

import (
	"cmp"
	"context"
	"fmt"
)

type Condition[T cmp.Ordered] interface {
	fmt.Stringer
	Match(context.Context, T) bool
}
