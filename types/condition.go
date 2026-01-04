// condition.go defines the Condition interface for generic filtering.

package types

import (
	"context"
	"fmt"
)

type Condition[T any] interface {
	fmt.Stringer
	Match(context.Context, T) bool
}
