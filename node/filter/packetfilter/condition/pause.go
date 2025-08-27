package condition

import (
	"context"
)

type PauseCond struct {
	OriginalCondition Condition
	ReleasePauseFn    context.CancelFunc
	WaitCh            <-chan struct{}
}

var _ Condition = (*PauseCond)(nil)

func Pause(ctx context.Context, originalCondition Condition) *PauseCond {
	ctx, cancelFn := context.WithCancel(ctx)
	return &PauseCond{
		OriginalCondition: originalCondition,
		ReleasePauseFn:    cancelFn,
		WaitCh:            ctx.Done(),
	}
}

func (f *PauseCond) String() string {
	return "Pause"
}

func (f *PauseCond) Match(
	ctx context.Context,
	in Input,
) bool {
	select {
	case <-ctx.Done():
		return false
	case <-f.WaitCh:
		if f.OriginalCondition == nil {
			return true
		}
		return f.OriginalCondition.Match(ctx, in)
	}
}
