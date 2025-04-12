package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/xsync"
)

type SeenAllStreamsT struct {
	Locker           xsync.Mutex
	SeenStreamsCount uint
	SeenStreamsMap   map[int]struct{}
}

func SeenAllStreams() *SeenAllStreamsT {
	return &SeenAllStreamsT{
		SeenStreamsMap: map[int]struct{}{},
	}
}

func (c *SeenAllStreamsT) Match(
	ctx context.Context,
	f frame.Input,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, f)
}

func (c *SeenAllStreamsT) match(
	ctx context.Context,
	f frame.Input,
) bool {
	c.acknowledgeInput(ctx, f)
	streamCount := c.getActiveStreamsCount()
	return streamCount >= uint(f.StreamsCount)
}

func (c *SeenAllStreamsT) String() string {
	return fmt.Sprintf("StreamAllStreams")
}

func (c *SeenAllStreamsT) acknowledgeInput(
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

func (c *SeenAllStreamsT) getActiveStreamsCount() uint {
	return c.SeenStreamsCount
}
