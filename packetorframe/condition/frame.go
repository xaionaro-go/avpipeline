// frame.go implements a condition that wraps a frame condition.

package condition

import (
	"context"

	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Frame framecondition.And

var _ Condition = Frame{}

func (v Frame) String() string {
	return framecondition.And(v).String()
}

func (v Frame) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	if in.Frame == nil {
		return false
	}
	return framecondition.And(v).Match(ctx, *in.Frame)
}
