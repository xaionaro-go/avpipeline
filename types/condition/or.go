// or.go implements a generic logical OR condition.

package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/types"
)

type Or[T any] []types.Condition[T]

func (s Or[T]) String() string {
	if len(s) == 1 {
		return s[0].String()
	}
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "|"))
}

func (s Or[T]) Match(ctx context.Context, v T) bool {
	for _, item := range s {
		if item.Match(ctx, v) {
			return true
		}
	}
	return false
}

func (s *Or[T]) Add(item types.Condition[T]) *Or[T] {
	*s = append(*s, item)
	return s
}
