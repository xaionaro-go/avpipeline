// panic.go implements a condition that panics with a message for packet input filtering.

package condition

import (
	"context"
	"fmt"
)

type Panic string

var _ Condition = Panic("")

func (f Panic) String() string {
	return fmt.Sprintf("Panic(%q)", string(f))
}

func (f Panic) Match(
	ctx context.Context,
	in Input,
) bool {
	panic(string(f))
}
