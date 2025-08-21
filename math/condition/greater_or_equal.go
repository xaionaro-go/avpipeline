package condition

import (
	"cmp"
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

func (cond GreaterOrEqualT[T]) Match(cmp T) bool {
	ref := cond.Getter.Get()
	return cmp >= ref
}

func (cond GreaterOrEqualT[T]) String() string {
	return fmt.Sprintf(">= %v", cond.Getter)
}
