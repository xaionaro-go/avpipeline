package extra

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

type PushQueueSizeCond struct {
	NodeSource    node.Abstract
	NodeSink      node.Abstract
	QueueSizeCond mathcondition.Condition[uint64]
}

var _ packetcondition.Condition = (*PushQueueSizeCond)(nil)

// Warning! This works only in assumption that nothing can filter packets
// between NodeSource and NodeSink except for pushing filters directly on the NodeSink.
func PushQueueSize(
	nodeSource, nodeSink node.Abstract,
	cond mathcondition.Condition[uint64],
) *PushQueueSizeCond {
	return &PushQueueSizeCond{
		QueueSizeCond: cond,
		NodeSource:    nodeSource,
		NodeSink:      nodeSink,
	}
}

func (c *PushQueueSizeCond) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	sourceStats := c.NodeSource.GetStatistics()
	sinkStats := c.NodeSink.GetStatistics()
	queueSizeBytes := 0 +
		sourceStats.BytesCountWrote.Load() -
		sinkStats.BytesCountRead.Load() -
		sinkStats.BytesCountMissed.Load()
	if proc, ok := c.NodeSink.GetProcessor().(processor.GetInternalQueueSizer); ok {
		if internalQueue := proc.GetInternalQueueSize(ctx); internalQueue != nil {
			logger.Tracef(ctx, "internal queue size: %d", *internalQueue)
			queueSizeBytes += *internalQueue
		}
	}
	logger.Tracef(ctx, "queue size: %d", queueSizeBytes)
	return c.QueueSizeCond.Match(uint64(queueSizeBytes))
}

func (c *PushQueueSizeCond) String() string {
	return fmt.Sprintf("NodeQueueSize(%v->%v, %s)", c.NodeSource, c.NodeSink, c.QueueSizeCond.String())
}
