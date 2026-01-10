// seen_streams_count.go implements a condition based on the number of streams seen so far.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

type SeenStreamsCountT struct {
	Is               condition.Condition[uint]
	Locker           xsync.Mutex
	SeenStreamsCount uint
	SeenStreamsMap   map[int]struct{}
}

var _ Condition = (*SeenStreamsCountT)(nil)

func SeenStreamCount(is condition.Condition[uint]) *SeenStreamsCountT {
	return &SeenStreamsCountT{
		Is:             is,
		SeenStreamsMap: map[int]struct{}{},
	}
}

func (c *SeenStreamsCountT) Match(
	ctx context.Context,
	pkt packetorframe.InputUnion,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, pkt)
}

func (c *SeenStreamsCountT) match(
	ctx context.Context,
	pkt packetorframe.InputUnion,
) bool {
	c.acknowledgeInput(ctx, pkt)
	streamCount := c.getActiveStreamsCount()
	logger.Tracef(ctx, "streamCount: %d", streamCount)
	return c.Is.Match(ctx, streamCount)
}

func (c *SeenStreamsCountT) String() string {
	return fmt.Sprintf("StreamCountIs(%v)", c.Is)
}

func (c *SeenStreamsCountT) acknowledgeInput(
	_ context.Context,
	pkt packetorframe.InputUnion,
) {
	streamIdx := pkt.GetStreamIndex()
	if _, ok := c.SeenStreamsMap[streamIdx]; ok {
		return
	}
	c.SeenStreamsCount++
	c.SeenStreamsMap[streamIdx] = struct{}{}
}

func (c *SeenStreamsCountT) getActiveStreamsCount() uint {
	return c.SeenStreamsCount
}
