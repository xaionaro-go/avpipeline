// and.go implements a logical AND condition for packet-or-frame filters.

package condition

import (
	"context"
	"strings"
)

type And []Condition

var _ Condition = And{}

func (v And) String() string {
	var parts []string
	for _, cond := range v {
		parts = append(parts, cond.String())
	}
	return "(" + strings.Join(parts, " && ") + ")"
}

func (v And) Match(ctx context.Context, in Input) bool {
	for _, cond := range v {
		if !cond.Match(ctx, in) {
			return false
		}
	}
	return true
}
