// and.go implements a generic logical AND condition.

package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/types"
)

type And[T any] []types.Condition[T]

func (s And[T]) String() string {
	if len(s) == 1 {
		return s[0].String()
	}
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And[T]) Match(ctx context.Context, v T) bool {
	for _, item := range s {
		if !item.Match(ctx, v) {
			return false
		}
	}
	return true
}

func (s *And[T]) Add(item types.Condition[T]) *And[T] {
	*s = append(*s, item)
	return s
}
