// pool.go implements a generic object pool with finalizers.

// Package pool provides a generic object pool with finalizers.
package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
)

var ReuseMemory = true

// Pool wraps sync.Pool with a typed reset hook and lifetime accounting.
// GetCount/PutCount track the running totals across the lifetime of the
// pool; tests use the (Get-Put) delta to detect leaked frames returned
// from the pool but never given back. The atomic counters add a single
// add per Get/Put which is negligible compared to a cgo Frame alloc.
type Pool[T any] struct {
	sync.Pool
	ResetFunc func(*T)
	GetCount  atomic.Int64
	PutCount  atomic.Int64
}

func NewPool[T any](
	allocFunc func() *T,
	resetFunc func(*T),
	freeFunc func(*T),
) *Pool[T] {
	return &Pool[T]{
		Pool: sync.Pool{
			New: func() any {
				v := allocFunc()
				runtime.SetFinalizer(v, func(v *T) {
					freeFunc(v)
				})
				return v
			},
		},
		ResetFunc: resetFunc,
	}
}

func (p *Pool[T]) Get() *T {
	p.GetCount.Add(1)
	v := p.Pool.Get().(*T)
	auditGet(v)
	return v
}

func (p *Pool[T]) Put(items ...*T) {
	if !ReuseMemory {
		return
	}
	for _, item := range items {
		p.ResetFunc(item)
		auditPut(item)
		p.Pool.Put(item)
		p.PutCount.Add(1)
	}
}
