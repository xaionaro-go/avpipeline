package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type SeenAllStreamsT struct {
	Locker           xsync.Mutex
	SeenStreamsCount uint
	SeenStreamsMap   map[int]struct{}
	ExpectedStreams  uint
}

func SeenAllStreams() *SeenAllStreamsT {
	return &SeenAllStreamsT{
		SeenStreamsMap: map[int]struct{}{},
	}
}

func (c *SeenAllStreamsT) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, pkt)
}

func (c *SeenAllStreamsT) match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	c.acknowledgeInput(ctx, pkt)
	streamCount := c.getActiveStreamsCount()
	return streamCount >= c.ExpectedStreams
}

func (c *SeenAllStreamsT) String() string {
	return fmt.Sprintf("StreamAllStreams")
}

func (c *SeenAllStreamsT) acknowledgeInput(
	ctx context.Context,
	pkt packet.Input,
) {
	if c.ExpectedStreams == 0 {
		pkt.Source.WithOutputFormatContext(ctx, func(fmtCtc *astiav.FormatContext) {
			c.ExpectedStreams = uint(fmtCtc.NbStreams())
		})
	}

	streamIdx := pkt.Stream.Index()
	if _, ok := c.SeenStreamsMap[streamIdx]; ok {
		return
	}
	c.SeenStreamsCount++
	c.SeenStreamsMap[streamIdx] = struct{}{}
}

func (c *SeenAllStreamsT) getActiveStreamsCount() uint {
	return c.SeenStreamsCount
}
