package condition

import (
	"context"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type MonotonicPTSConvertedType struct {
	xsync.Map[int, *xsync.WithMutex[time.Duration]]
}

var _ Condition = (*MonotonicPTSConvertedType)(nil)

func MonotonicPTSConverted() *MonotonicPTSConvertedType {
	return &MonotonicPTSConvertedType{}
}

func (c *MonotonicPTSConvertedType) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	ptsConverted := pkt.PtsAsDuration()
	streamIdx := pkt.StreamIndex()
	val, ok := c.LoadOrStore(streamIdx, &xsync.WithMutex[time.Duration]{})
	assert(ctx, val != nil, ok, streamIdx, ptsConverted)
	return xsync.DoR1(ctx, val, func() bool {
		if ptsConverted <= val.Value {
			logger.Tracef(ctx, "MonotonicPTSConverted: dropping packet with PTS %v <= last PTS %v", ptsConverted, val.Value)
			return false
		}
		val.Value = ptsConverted
		return true
	})
}

func (c *MonotonicPTSConvertedType) String() string {
	return "MonotonicPTSConverted"
}
