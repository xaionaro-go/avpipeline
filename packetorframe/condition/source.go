// source.go implements a condition that matches the source of a packet.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type SourceCond struct {
	Expected packetorframe.AbstractSource
}

var _ Condition = (*SourceCond)(nil)

func Source(expected packetorframe.AbstractSource) SourceCond {
	return SourceCond{Expected: expected}
}

func (c SourceCond) String() string {
	return fmt.Sprintf("PacketSource(%s)", c.Expected)
}

func (c SourceCond) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	pkt := in.Packet
	if pkt == nil {
		return false
	}
	return pkt.Source == c.Expected
}
