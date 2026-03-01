// combine.go provides a helper to combine multiple conditions.

package condition

import (
	"github.com/xaionaro-go/avpipeline/types"
)

// CombineConds combines multiple conditions into a single condition.
// Returns nil if no conditions, the single condition if one, or an And of all conditions.
func CombineConds[T any](conds ...types.Condition[T]) types.Condition[T] {
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	}
	return And[T](conds)
}
