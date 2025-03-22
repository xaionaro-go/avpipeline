package kernel

import (
	"sync"
)

type closeChan struct {
	closeOnce sync.Once
	c         chan struct{}
}

func newCloseChan() *closeChan {
	return &closeChan{
		c: make(chan struct{}),
	}
}

func (c *closeChan) CloseChan() <-chan struct{} {
	return c.c
}

func (c *closeChan) Close() {
	c.closeOnce.Do(func() {
		close(c.c)
	})
}

func (c *closeChan) IsClosed() bool {
	select {
	case <-c.c:
		return true
	default:
		return false
	}
}
