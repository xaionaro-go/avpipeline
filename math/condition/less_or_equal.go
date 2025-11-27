package condition

import (
	"cmp"
	"context"
	"fmt"
)

type LessOrEqualT[T cmp.Ordered] struct {
	Getter Getter[T]
}

var _ Condition[int] = LessOrEqualT[int]{}

func LessOrEqualVariable[T cmp.Ordered](getter Getter[T]) LessOrEqualT[T] {
	return LessOrEqualT[T]{
		Getter: getter,
	}
}

func LessOrEqual[T cmp.Ordered](ref T) LessOrEqualT[T] {
	return LessOrEqualT[T]{
		Getter: GetterStatic[T]{ref},
	}
}

func (cond LessOrEqualT[T]) Match(
	ctx context.Context,
	cmp T,
) bool {
	ref := cond.Getter.Get(ctx)
	return cmp <= ref
}

func (cond LessOrEqualT[T]) String() string {
	return fmt.Sprintf("<= %v", cond.Getter)
}
