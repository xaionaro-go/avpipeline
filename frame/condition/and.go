package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/frame"
)

type And []Condition

var _ Condition = (And)(nil)

func (s *And) Add(item Condition) *And {
	*s = append(*s, item)
	return s
}

func (s And) String() string {
	if len(s) == 1 {
		return s[0].String()
	}
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And) Match(
	ctx context.Context,
	f frame.Input,
) bool {
	for _, item := range s {
		if !item.Match(ctx, f) {
			return false
		}
	}
	return true
}
