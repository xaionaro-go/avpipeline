package resetter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeResetter struct {
	calls atomic.Int32
	err   error
	fn    func(context.Context) error
}

func (r *fakeResetter) Reset(ctx context.Context) error {
	r.calls.Add(1)
	if r.fn != nil {
		return r.fn(ctx)
	}
	return r.err
}

type manualDeadlineContext struct {
	parent context.Context
	done   chan struct{}
	once   sync.Once
	err    atomic.Value
}

func newManualDeadlineContext(parent context.Context) *manualDeadlineContext {
	return &manualDeadlineContext{
		parent: parent,
		done:   make(chan struct{}),
	}
}

func (ctx *manualDeadlineContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *manualDeadlineContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *manualDeadlineContext) Err() error {
	err, _ := ctx.err.Load().(error)
	return err
}

func (ctx *manualDeadlineContext) Value(key any) any {
	return ctx.parent.Value(key)
}

func (ctx *manualDeadlineContext) expire() {
	ctx.err.Store(context.DeadlineExceeded)
	ctx.once.Do(func() {
		close(ctx.done)
	})
}

func TestRunSkipsTimedOutResetterAndContinuesDeterministically(t *testing.T) {
	ctx := context.Background()
	timedOutCtx := newManualDeadlineContext(ctx)

	blocker := &fakeResetter{
		fn: func(resetCtx context.Context) error {
			timedOutCtx.expire()
			<-resetCtx.Done()
			return nil
		},
	}
	follower := &fakeResetter{}
	resetters := []Named{
		{Name: "Blocker", Resetter: blocker},
		{Name: "Follower", Resetter: follower},
	}

	contexts := []context.Context{timedOutCtx, ctx}
	err := run(ctx, "owner", time.Second, resetters, func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		require.NotZero(t, timeout)
		resetCtx := contexts[0]
		contexts = contexts[1:]
		return resetCtx, func() {}
	})

	require.NoError(t, err)
	require.Equal(t, int32(1), blocker.calls.Load())
	require.Equal(t, int32(1), follower.calls.Load())
	require.Empty(t, contexts)
}

func TestRunJoinsNonTimeoutErrorsAndContinues(t *testing.T) {
	ctx := context.Background()
	firstErr := errors.New("first reset failed")
	secondErr := errors.New("second reset failed")
	first := &fakeResetter{err: firstErr}
	second := &fakeResetter{err: secondErr}
	third := &fakeResetter{}

	err := Run(ctx, "owner", time.Second, []Named{
		{Name: "First", Resetter: first},
		{Name: "nil"},
		{Name: "Second", Resetter: second},
		{Name: "Third", Resetter: third},
	})

	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.Equal(t, int32(1), first.calls.Load())
	require.Equal(t, int32(1), second.calls.Load())
	require.Equal(t, int32(1), third.calls.Load())
}

func TestRunOuterContextCancellationIsNotPerResetTimeout(t *testing.T) {
	outerCtx := newManualDeadlineContext(context.Background())
	outerCtx.expire()
	resetCtx := newManualDeadlineContext(outerCtx)
	resetCtx.expire()
	probe := &fakeResetter{
		fn: func(ctx context.Context) error {
			return ctx.Err()
		},
	}

	err := run(outerCtx, "owner", time.Second, []Named{{Name: "Probe", Resetter: probe}}, func(context.Context, time.Duration) (context.Context, context.CancelFunc) {
		return resetCtx, func() {}
	})

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), probe.calls.Load())
}

func TestRunRejectsInvalidTimeout(t *testing.T) {
	ctx := context.Background()
	probe := &fakeResetter{}

	err := Run(ctx, "owner", 0, []Named{{Name: "Probe", Resetter: probe}})

	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidTimeout)
	require.Equal(t, int32(0), probe.calls.Load())
}
