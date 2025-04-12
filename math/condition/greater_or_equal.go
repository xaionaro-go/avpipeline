package condition

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

type GreaterOrEqualT[T constraints.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = GreaterOrEqualT[int]{}

func GreaterOrEqualVariable[T constraints.Ordered](getter Getter[T]) GreaterOrEqualT[T] {
	return GreaterOrEqualT[T]{
		Getter: getter,
	}
}

func GreaterOrEqual[T constraints.Ordered](ref T) GreaterOrEqualT[T] {
	return GreaterOrEqualT[T]{
		Getter: GetterStatic[T]{ref},
	}
}

func (cond GreaterOrEqualT[T]) Match(cmp T) bool {
	ref := cond.Getter.Get()
	return cmp >= ref
}

func (cond GreaterOrEqualT[T]) String() string {
	return fmt.Sprintf(">= %v", cond.Getter)
}
