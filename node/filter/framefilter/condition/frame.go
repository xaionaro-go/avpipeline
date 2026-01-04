// frame.go implements a condition that wraps frame-specific conditions for node input filtering.

package condition

import (
	"context"

	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
)

type Frame framecondition.And

var _ Condition = Frame{}

func (v Frame) String() string {
	return framecondition.And(v).String()
}

func (v Frame) Match(ctx context.Context, in Input) bool {
	return framecondition.And(v).Match(ctx, in.Input)
}
