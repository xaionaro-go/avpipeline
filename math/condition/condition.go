package condition

import (
	"cmp"
	"fmt"
)

type Condition[T cmp.Ordered] interface {
	fmt.Stringer
	Match(T) bool
}
