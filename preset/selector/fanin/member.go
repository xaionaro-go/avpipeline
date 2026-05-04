package fanin

import "context"

type Member interface {
	Pause(ctx context.Context) error
	Unpause(ctx context.Context) error
	IsPaused(ctx context.Context) bool
}

type AsyncErrorHandler func(ctx context.Context, err error)
