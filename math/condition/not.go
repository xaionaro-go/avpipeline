package condition

import (
	"cmp"
	"fmt"
)

type Not[T cmp.Ordered] []Condition[T]

var _ Condition[int] = (*Not[int])(nil)

func (n Not[T]) String() string {
	if len(n) == 1 {
		return fmt.Sprintf("Not(%s)", n[0])
	}
	return fmt.Sprintf("Not(%s)", And[T](n))
}

func (n Not[T]) Match(
	v T,
) bool {
	return !And[T](n).Match(v)
}
