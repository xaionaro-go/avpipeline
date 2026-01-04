// stream_index.go implements a condition that checks the stream index of a packet.

package condition

import (
	"context"
	"fmt"

	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packet"
)

type StreamIndexT struct {
	mathcondition.Condition[int]
}

func StreamIndex(
	cond mathcondition.Condition[int],
) *StreamIndexT {
	return &StreamIndexT{
		Condition: cond,
	}
}

func (c *StreamIndexT) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return c.Condition.Match(ctx, pkt.GetStreamIndex())
}

func (c *StreamIndexT) String() string {
	return fmt.Sprintf("StreamIndex(%s)", c.Condition)
}
