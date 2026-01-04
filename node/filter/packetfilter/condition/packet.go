// packet.go implements a condition that wraps packet-specific conditions for node input filtering.

package condition

import (
	"context"

	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
)

type Packet packetcondition.And

var _ Condition = Packet{}

func (v Packet) String() string {
	return packetcondition.And(v).String()
}

func (v Packet) Match(ctx context.Context, in Input) bool {
	return packetcondition.And(v).Match(ctx, in.Input)
}
