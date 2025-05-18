package condition

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

type EqualT[T constraints.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = EqualT[int]{}

func EqualVariable[T constraints.Ordered](getter Getter[T]) EqualT[T] {
	return EqualT[T]{
		Getter: getter,
	}
}

func Equal[T constraints.Ordered](ref T) EqualT[T] {
	return EqualT[T]{
		Getter: GetterStatic[T]{ref},
	}
}

func (cond EqualT[T]) Match(cmp T) bool {
	ref := cond.Getter.Get()
	return cmp >= ref
}

func (cond EqualT[T]) String() string {
	return fmt.Sprintf("== %v", cond.Getter)
}
