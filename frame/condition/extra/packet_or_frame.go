package extra

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type PacketOrFrame packetorframecondition.And

var _ condition.Condition = PacketOrFrame{}

func (v PacketOrFrame) String() string {
	return packetorframecondition.And(v).String()
}

func (v PacketOrFrame) Match(ctx context.Context, in frame.Input) bool {
	return packetorframecondition.And(v).Match(ctx, packetorframe.InputUnion{
		Frame: &in,
	})
}
