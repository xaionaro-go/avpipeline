// push_queue_size.go implements a condition based on the push queue size of a node.

// Package condition provides extra conditions for packet-or-frame unions.
package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

type PushQueueSizeCond struct {
	NodeSink      node.Abstract
	QueueSizeCond mathcondition.Condition[uint64]
}

var _ condition.Condition = (*PushQueueSizeCond)(nil)

func PushQueueSize(
	nodeSink node.Abstract,
	cond mathcondition.Condition[uint64],
) *PushQueueSizeCond {
	return &PushQueueSizeCond{
		NodeSink:      nodeSink,
		QueueSizeCond: cond,
	}
}

func (c *PushQueueSizeCond) Match(
	ctx context.Context,
	_ packetorframe.InputUnion,
) bool {
	nodeStats := c.NodeSink.GetCountersPtr()
	procStats := c.NodeSink.GetProcessor().CountersPtr()
	totalReceived := nodeStats.Received.TotalBytes()
	totalProcessed := procStats.Processed.TotalBytes()
	queueSizeBytes := totalReceived - totalProcessed
	if proc, ok := c.NodeSink.GetProcessor().(processor.GetInternalQueueSizer); ok {
		if internalQueue := proc.GetInternalQueueSize(ctx); internalQueue != nil {
			logger.Tracef(ctx, "internal queue size: %v", internalQueue)
			for _, internalQueue := range internalQueue {
				queueSizeBytes += internalQueue
			}
		}
	}
	logger.Tracef(ctx, "queue size: %d", queueSizeBytes)
	return c.QueueSizeCond.Match(ctx, uint64(queueSizeBytes))
}

func (c *PushQueueSizeCond) String() string {
	return fmt.Sprintf("NodeQueueSize(%v: %s)", c.NodeSink, c.QueueSizeCond.String())
}
