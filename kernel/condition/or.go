package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type Or[K types.Abstract] []Condition[K]

var _ Condition[types.Abstract] = (Or[types.Abstract])(nil)

func (s *Or[K]) Add(item Condition[K]) *Or[K] {
	*s = append(*s, item)
	return s
}

func (s Or[K]) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "|"))
}

func (s Or[K]) Match(
	ctx context.Context,
	kernel K,
) bool {
	for _, item := range s {
		if item.Match(ctx, kernel) {
			return true
		}
	}
	return false
}
