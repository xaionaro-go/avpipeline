package types

import (
	"context"
	"fmt"
)

type Condition[T any] interface {
	fmt.Stringer
	Match(context.Context, T) bool
}

/* for easier copy&paste:

func (c *) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {

}

func (c *) String() string {

}

*/
