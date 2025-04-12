package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/xsync"
)

type SeenStreamsCountT struct {
	Is               condition.Condition[uint]
	Locker           xsync.Mutex
	SeenStreamsCount uint
	SeenStreamsMap   map[int]struct{}
}

func SeenStreamCount(is condition.Condition[uint]) *SeenStreamsCountT {
	return &SeenStreamsCountT{
		Is:             is,
		SeenStreamsMap: map[int]struct{}{},
	}
}

func (c *SeenStreamsCountT) Match(
	ctx context.Context,
	f frame.Input,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, f)
}

func (c *SeenStreamsCountT) match(
	ctx context.Context,
	f frame.Input,
) bool {
	c.acknowledgeInput(ctx, f)
	streamCount := c.getActiveStreamsCount()
	return c.Is.Match(streamCount)
}

func (c *SeenStreamsCountT) String() string {
	return fmt.Sprintf("StreamCountIs(%v)", c.Is)
}

func (c *SeenStreamsCountT) acknowledgeInput(
	_ context.Context,
	f frame.Input,
) {
	streamIdx := f.StreamIndex
	if _, ok := c.SeenStreamsMap[streamIdx]; ok {
		return
	}
	c.SeenStreamsCount++
	c.SeenStreamsMap[streamIdx] = struct{}{}
}

func (c *SeenStreamsCountT) getActiveStreamsCount() uint {
	return c.SeenStreamsCount
}
