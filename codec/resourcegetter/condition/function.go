package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/resourcegetter"
)

type Function func(context.Context, resourcegetter.Input) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, f resourcegetter.Input) bool {
	return fn(ctx, f)
}
