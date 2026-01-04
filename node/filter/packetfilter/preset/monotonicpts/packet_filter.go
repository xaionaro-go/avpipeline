// packet_filter.go implements a packet filter preset that ensures monotonic PTS.

// Package monotonicpts provides a packet filter preset that ensures monotonic PTS.
package monotonicpts

import (
	"context"
	"fmt"

	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
)

type PacketFilter struct {
	*monotonicpts.Filter
}

var _ packetfiltercondition.Condition = (*PacketFilter)(nil)

func New(shouldCorrect bool) *PacketFilter {
	return &PacketFilter{
		Filter: monotonicpts.New(shouldCorrect),
	}
}

func (f *PacketFilter) String() string {
	return fmt.Sprintf("MonotonicPTS(%s)", f.Filter.LatestPTS)
}

func (f *PacketFilter) Match(
	ctx context.Context,
	in packetfiltercondition.Input,
) bool {
	return f.Filter.Match(ctx, packetorframe.InputUnion{
		Packet: &in.Input,
	})
}
