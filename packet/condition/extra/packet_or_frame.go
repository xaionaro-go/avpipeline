// packet_or_frame.go implements a condition that works on both packets and frames.

package extra

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type PacketOrFrame packetorframecondition.And

var _ condition.Condition = PacketOrFrame{}

func (v PacketOrFrame) String() string {
	return packetorframecondition.And(v).String()
}

func (v PacketOrFrame) Match(ctx context.Context, in packet.Input) bool {
	return packetorframecondition.And(v).Match(ctx, packetorframe.InputUnion{
		Packet: &in,
	})
}
