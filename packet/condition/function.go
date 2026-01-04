// function.go implements a condition based on a custom function.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type Function func(context.Context, packet.Input) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, pkt packet.Input) bool {
	return fn(ctx, pkt)
}
