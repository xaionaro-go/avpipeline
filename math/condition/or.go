package condition

import (
	"cmp"
	"fmt"
	"strings"
)

type Or[T cmp.Ordered] []Condition[T]

var _ Condition[int] = (Or[int])(nil)

func (s *Or[T]) Add(item Condition[T]) *Or[T] {
	*s = append(*s, item)
	return s
}

func (s Or[T]) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "|"))
}

func (s Or[T]) Match(
	v T,
) bool {
	for _, item := range s {
		if item.Match(v) {
			return true
		}
	}
	return false
}
