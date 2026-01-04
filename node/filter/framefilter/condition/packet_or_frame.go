// packet_or_frame.go implements a condition that works on both packets and frames for node input filtering.

package condition

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type PacketOrFrame packetorframecondition.And

var _ Condition = PacketOrFrame{}

func (v PacketOrFrame) String() string {
	return packetorframecondition.And(v).String()
}

func (v PacketOrFrame) Match(ctx context.Context, in Input) bool {
	return packetorframecondition.And(v).Match(ctx, packetorframe.InputUnion{
		Frame: &in.Input,
	})
}
