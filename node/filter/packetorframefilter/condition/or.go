// or.go implements a logical OR condition for packet-or-frame filters.

package condition

import (
	"context"
	"strings"
)

type Or []Condition

var _ Condition = Or{}

func (v Or) String() string {
	var parts []string
	for _, cond := range v {
		parts = append(parts, cond.String())
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

func (v Or) Match(ctx context.Context, in Input) bool {
	for _, cond := range v {
		if cond.Match(ctx, in) {
			return true
		}
	}
	return false
}
