// greater_or_equal.go implements a "greater than or equal to" condition for mathematical comparisons.

package condition

import (
	"cmp"
	"context"
	"fmt"
)

type GreaterOrEqualT[T cmp.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = GreaterOrEqualT[int]{}

func GreaterOrEqualVariable[T cmp.Ordered](getter Getter[T]) GreaterOrEqualT[T] {
	return GreaterOrEqualT[T]{
		Getter: getter,
	}
}

func GreaterOrEqual[T cmp.Ordered](ref T) GreaterOrEqualT[T] {
	return GreaterOrEqualT[T]{
		Getter: GetterStatic[T]{ref},
	}
}

func (cond GreaterOrEqualT[T]) Match(
	ctx context.Context,
	cmp T,
) bool {
	ref := cond.Getter.Get(ctx)
	return cmp >= ref
}

func (cond GreaterOrEqualT[T]) String() string {
	return fmt.Sprintf(">= %v", cond.Getter)
}
