package condition

import (
	"fmt"
)

type Getter[T any] interface {
	fmt.Stringer
	Get() T
}

type GetterStatic[T any] struct {
	StaticValue T
}

var _ Getter[int] = GetterStatic[int]{}

func (g GetterStatic[T]) Get() T {
	return g.StaticValue
}

func (g GetterStatic[T]) String() string {
	return fmt.Sprintf("%v", g.StaticValue)
}

type GetterFunction[T any] func() T

var _ Getter[int] = GetterStatic[int]{}

func (g GetterFunction[T]) Get() T {
	return g()
}

func (g GetterFunction[T]) String() string {
	return "func"
}
