package condition

import (
	"cmp"
	"context"
	"fmt"
	"strings"
)

type And[T cmp.Ordered] []Condition[T]

var _ Condition[int] = (And[int])(nil)

func (s *And[T]) Add(item Condition[T]) *And[T] {
	*s = append(*s, item)
	return s
}

func (s And[T]) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And[T]) Match(
	ctx context.Context,
	v T,
) bool {
	for _, item := range s {
		if !item.Match(ctx, v) {
			return false
		}
	}
	return true
}
