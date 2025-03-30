package pool

import (
	"runtime"
	"sync"
)

var ReuseMemory = true

type Pool[T any] struct {
	sync.Pool
	ResetFunc func(*T)
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
	return p.Pool.Get().(*T)
}

func (p *Pool[T]) Put(item *T) {
	if !ReuseMemory {
		return
	}
	p.ResetFunc(item)
	p.Pool.Put(item)
}
