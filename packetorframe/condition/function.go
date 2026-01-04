// function.go implements a condition that uses a custom function.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Function func(context.Context, packetorframe.InputUnion) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	return fn(ctx, in)
}
