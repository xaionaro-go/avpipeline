package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type PacketSourceCond struct {
	Expected packet.Source
}

var _ Condition = (*PacketSourceCond)(nil)

func PacketSource(expected packet.Source) PacketSourceCond {
	return PacketSourceCond{Expected: expected}
}

func (c PacketSourceCond) String() string {
	return fmt.Sprintf("PacketSource(%s)", c.Expected)
}

func (c PacketSourceCond) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	pkt := in.Packet
	if pkt == nil {
		return false
	}
	return pkt.Source == c.Expected
}
