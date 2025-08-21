package condition

import (
	"cmp"
	"fmt"
)

type EqualT[T cmp.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = EqualT[int]{}

func EqualVariable[T cmp.Ordered](getter Getter[T]) EqualT[T] {
	return EqualT[T]{
		Getter: getter,
	}
}

func Equal[T cmp.Ordered](ref T) EqualT[T] {
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
