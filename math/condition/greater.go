// greater.go implements a "greater than" condition for mathematical comparisons.

package condition

import (
	"cmp"
	"context"
	"fmt"
)

type GreaterT[T cmp.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = GreaterT[int]{}

func GreaterVariable[T cmp.Ordered](getter Getter[T]) GreaterT[T] {
	return GreaterT[T]{
		Getter: getter,
	}
}

func Greater[T cmp.Ordered](ref T) GreaterT[T] {
	return GreaterT[T]{
		Getter: GetterStatic[T]{ref},
	}
}

func (cond GreaterT[T]) Match(
	ctx context.Context,
	cmp T,
) bool {
	ref := cond.Getter.Get(ctx)
	return cmp > ref
}

func (cond GreaterT[T]) String() string {
	return fmt.Sprintf("> %v", cond.Getter)
}
