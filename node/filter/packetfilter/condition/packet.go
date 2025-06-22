package condition

import (
	"context"

	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
)

type Packet struct {
	Condition packetcondition.Condition
}

var _ Condition = Packet{}

func (v Packet) String() string {
	return v.Condition.String()
}

func (v Packet) Match(ctx context.Context, in Input) bool {
	return v.Condition.Match(ctx, in.Input)
}
