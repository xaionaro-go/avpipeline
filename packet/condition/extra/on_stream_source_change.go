package extra

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type OnStreamSourceChangeHandler interface {
	OnStreamSourceChange(ctx context.Context, pkt packet.Input, prevSource packet.Source)
}

type OnStreamSourceChange struct {
	PreviousStreamSource map[int]packet.Source
	Handler              OnStreamSourceChangeHandler
	Locker               xsync.Mutex
}

var _ condition.Condition = (*OnStreamSourceChange)(nil)

func NewOnStreamSourceChange(handler OnStreamSourceChangeHandler) *OnStreamSourceChange {
	return &OnStreamSourceChange{
		PreviousStreamSource: make(map[int]packet.Source),
		Handler:              handler,
	}
}

func (c *OnStreamSourceChange) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, pkt)
}

func (c *OnStreamSourceChange) match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	if pkt.Source == nil {
		return true
	}
	streamIndex := pkt.GetStreamIndex()
	prevSource := c.PreviousStreamSource[streamIndex]
	if pkt.Source == prevSource {
		return true
	}
	logger.Debugf(ctx, "stream source for stream #%d changed from %s to %s", streamIndex, prevSource, pkt.Source)
	c.Handler.OnStreamSourceChange(ctx, pkt, prevSource)
	c.PreviousStreamSource[streamIndex] = pkt.Source
	return true
}

func (c *OnStreamSourceChange) String() string {
	return "OnStreamSourceChange"
}
