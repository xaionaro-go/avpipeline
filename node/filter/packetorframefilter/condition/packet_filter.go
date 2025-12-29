package condition

import (
	"context"
	"fmt"

	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

type PacketFilter struct {
	packetfiltercondition.Condition
}

var _ Condition = PacketFilter{}

func (v PacketFilter) String() string {
	return fmt.Sprintf("PacketFilter(%s)", v.Condition)
}

func (v PacketFilter) Match(ctx context.Context, in Input) bool {
	if in.Input.Packet == nil {
		return false
	}
	if v.Condition == nil {
		return true
	}
	return v.Condition.Match(ctx, packetfiltercondition.Input{
		Destination: in.Destination,
		Input:       *in.Input.Packet,
	})
}
