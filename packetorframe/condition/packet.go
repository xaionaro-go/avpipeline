package condition

import (
	"context"

	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Packet packetcondition.And

var _ Condition = Packet{}

func (v Packet) String() string {
	return packetcondition.And(v).String()
}

func (v Packet) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	if in.Packet == nil {
		return false
	}
	return packetcondition.And(v).Match(ctx, *in.Packet)
}
