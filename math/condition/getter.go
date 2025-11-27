package condition

import (
	"context"
	"fmt"
)

type Getter[T any] interface {
	fmt.Stringer
	Get(context.Context) T
}

type GetterStatic[T any] struct {
	StaticValue T
}

var _ Getter[int] = GetterStatic[int]{}

func (g GetterStatic[T]) Get(context.Context) T {
	return g.StaticValue
}

func (g GetterStatic[T]) String() string {
	return fmt.Sprintf("%v", g.StaticValue)
}

type GetterFunction[T any] func(context.Context) T

var _ Getter[int] = GetterStatic[int]{}

func (g GetterFunction[T]) Get(ctx context.Context) T {
	return g(ctx)
}

func (g GetterFunction[T]) String() string {
	return "func"
}
