package condition

import (
	"context"

	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
)

type Frame struct {
	Condition framecondition.Condition
}

var _ Condition = (*Frame)(nil)

func (v *Frame) String() string {
	return v.Condition.String()
}

func (v *Frame) Match(ctx context.Context, in Input) bool {
	return v.Condition.Match(ctx, in.Input)
}
