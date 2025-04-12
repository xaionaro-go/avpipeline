package condition

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

type Condition[T constraints.Ordered] interface {
	fmt.Stringer
	Match(T) bool
}
