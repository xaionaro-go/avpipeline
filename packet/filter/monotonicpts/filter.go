// filter.go implements a packet filter that ensures monotonic PTS.

// Package monotonicpts provides a packet filter that ensures monotonic PTS.
package monotonicpts

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
)

type Filter struct {
	*monotonicpts.Filter
}

var _ condition.Condition = (*Filter)(nil)

func New(shouldCorrect bool) *Filter {
	return &Filter{
		Filter: monotonicpts.New(shouldCorrect),
	}
}

func (f *Filter) String() string {
	return fmt.Sprintf("MonotonicPTS(%s)", f.Filter.LatestPTS)
}

func (f *Filter) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return f.Filter.Match(ctx, packetorframe.InputUnion{
		Packet: &pkt,
	})
}
