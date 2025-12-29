package condition

import (
	"context"

	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type PacketOrFrame packetorframecondition.And

var _ Condition = PacketOrFrame{}

func (v PacketOrFrame) String() string {
	return packetorframecondition.And(v).String()
}

func (v PacketOrFrame) Match(ctx context.Context, in Input) bool {
	return packetorframecondition.And(v).Match(ctx, in.Input)
}
