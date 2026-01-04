// function.go implements a condition based on a custom function.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
)

type Function func(context.Context, frame.Input) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, f frame.Input) bool {
	return fn(ctx, f)
}
