// condition.go defines the Condition interface for mathematical comparisons.

// Package condition provides various mathematical conditions for filtering.
package condition

import (
	"cmp"
	"context"
	"fmt"
)

type Condition[T cmp.Ordered] interface {
	fmt.Stringer
	Match(context.Context, T) bool
}
