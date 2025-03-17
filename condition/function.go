package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Function func(context.Context, types.InputPacket) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, pkt types.InputPacket) bool {
	return fn(ctx, pkt)
}
