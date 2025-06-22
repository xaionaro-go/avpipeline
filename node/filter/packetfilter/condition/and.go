package condition

import (
	"context"
	"fmt"
	"strings"
)

type And []Condition

var _ Condition = (And)(nil)

func (s *And) Add(item Condition) *And {
	*s = append(*s, item)
	return s
}

func (s And) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And) Match(
	ctx context.Context,
	in Input,
) bool {
	for _, item := range s {
		if !item.Match(ctx, in) {
			return false
		}
	}
	return true
}
