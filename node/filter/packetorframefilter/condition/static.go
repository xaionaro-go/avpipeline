// static.go implements a static boolean condition.

package condition

import (
	"context"
	"fmt"
)

type Static bool

var _ Condition = (Static)(false)

func (v Static) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static) Match(context.Context, Input) bool {
	return (bool)(v)
}
