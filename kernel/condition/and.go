package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type And[K types.Abstract] []Condition[K]

var _ Condition[types.Abstract] = (And[types.Abstract])(nil)

func (s *And[K]) Add(item Condition[K]) *And[K] {
	*s = append(*s, item)
	return s
}

func (s And[K]) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And[K]) Match(
	ctx context.Context,
	kernel K,
) bool {
	for _, item := range s {
		if !item.Match(ctx, kernel) {
			return false
		}
	}
	return true
}
