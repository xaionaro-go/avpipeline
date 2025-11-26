package gettercondition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/codec/resource"
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
	f resource.GetterInput,
) bool {
	for _, item := range s {
		if !item.Match(ctx, f) {
			return false
		}
	}
	return true
}
