package condition

import (
	"context"
	"fmt"

	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
)

type FrameFilter struct {
	framefiltercondition.Condition
}

var _ Condition = FrameFilter{}

func (v FrameFilter) String() string {
	return fmt.Sprintf("FrameFilter(%s)", v.Condition)
}

func (v FrameFilter) Match(ctx context.Context, in Input) bool {
	if in.Input.Frame == nil {
		return false
	}
	if v.Condition == nil {
		return true
	}
	return v.Condition.Match(ctx, framefiltercondition.Input{
		Destination: in.Destination,
		Input:       *in.Input.Frame,
	})
}
