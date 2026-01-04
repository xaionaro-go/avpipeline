// closure_signaler.go provides a utility for signaling the closure of a resource.

// Package closuresignaler provides a utility for signaling the closure of a resource.
package closuresignaler

import (
	"context"
	"sync"

	"github.com/xaionaro-go/avpipeline/logger"
)

type ClosureSignaler struct {
	closeOnce sync.Once
	c         chan struct{}
}

func New() *ClosureSignaler {
	return &ClosureSignaler{
		c: make(chan struct{}),
	}
}

func (c *ClosureSignaler) CloseChan() <-chan struct{} {
	return c.c
}

func (c *ClosureSignaler) Close(ctx context.Context) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close") }()
	c.closeOnce.Do(func() {
		close(c.c)
	})
}

func (c *ClosureSignaler) IsClosed() bool {
	select {
	case <-c.c:
		return true
	default:
		return false
	}
}
