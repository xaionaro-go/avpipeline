package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoute_String(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/path")
	assert.Equal(t, "test/path", route.String())
}

func TestRoute_String_NilSafe(t *testing.T) {
	var route *Route[any]
	assert.Equal(t, "<nil>", route.String())
}

func TestRoute_IsOpen(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()
	assert.True(t, route.IsOpen(ctx), "newly created route should be open")
}

func TestRoute_Close(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	err := route.Close(ctx)
	assert.NoError(t, err)
}

func TestRoute_AddPublisher_ExclusiveTakeover(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	publishers, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
	assert.Same(t, pub, publishers[0])
}

func TestRoute_AddPublisher_ExclusiveFail(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveFail)
	publishers, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
}

func TestRoute_AddPublisher_SharedTakeover(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeSharedTakeover)
	publishers, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
}

func TestRoute_AddPublisher_SharedFail(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeSharedFail)
	publishers, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
}

func TestRoute_AddPublisher_NilPublisher_ReturnsError(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	var wg sync.WaitGroup
	var retErr error
	route.Locker().Do(ctx, func() {
		_, retErr = route.AddPublisherLocked(ctx, nil, &wg)
	})
	wg.Wait()
	assert.Error(t, retErr)
	assert.Contains(t, retErr.Error(), "nil")
}

func TestRoute_AddPublisher_DuplicatePublisher(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, pub, &wg)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrAlreadyAPublisher{}))
	})
	wg.Wait()
}

func TestRoute_AddPublisher_ExclusiveTakeover_ReplacesExisting(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub1 := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	pub2 := newMockPublisher("pub2", PublishModeExclusiveTakeover)
	publishers, err := route.AddPublisher(ctx, pub2)
	require.NoError(t, err)

	// Wait for the async close of pub1.
	time.Sleep(100 * time.Millisecond)

	require.Len(t, publishers, 1)
	assert.Same(t, pub2, publishers[0])
	assert.GreaterOrEqual(t, pub1.getCloseCount(), 1, "old publisher should have been closed")
}

func TestRoute_AddPublisher_ExclusiveFail_ConflictReturnsError(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub1 := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	pub2 := newMockPublisher("pub2", PublishModeExclusiveFail)

	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, pub2, &wg)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrAlreadyHasPublisher{}))
	})
	wg.Wait()
}

func TestRoute_AddPublisher_SharedTakeover_ReplacesExclusivePublisher(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Add an exclusive publisher first.
	exclusivePub := newMockPublisher("exclusive", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, exclusivePub)
	require.NoError(t, err)

	// Shared takeover should replace the exclusive publisher.
	sharedPub := newMockPublisher("shared", PublishModeSharedTakeover)

	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		publishers, err2 := route.AddPublisherLocked(ctx, sharedPub, &wg)
		require.NoError(t, err2)
		require.Len(t, publishers, 1)
		assert.Same(t, sharedPub, publishers[0])
	})
	wg.Wait()

	// Wait for async close of exclusive publisher.
	time.Sleep(100 * time.Millisecond)
	assert.True(t, exclusivePub.isClosed(), "exclusive publisher should have been closed")
}

func TestRoute_AddPublisher_SharedFail_FailsWhenExclusiveExists(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	exclusivePub := newMockPublisher("exclusive", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, exclusivePub)
	require.NoError(t, err)

	sharedPub := newMockPublisher("shared", PublishModeSharedFail)

	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, sharedPub, &wg)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrAlreadyHasPublisher{}))
	})
	wg.Wait()
}

func TestRoute_AddPublisher_SharedTakeover_KeepsOtherShared(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	shared1 := newMockPublisher("shared1", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, shared1)
	require.NoError(t, err)

	shared2 := newMockPublisher("shared2", PublishModeSharedTakeover)
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		publishers, err2 := route.AddPublisherLocked(ctx, shared2, &wg)
		require.NoError(t, err2)
		require.Len(t, publishers, 2)
	})
	wg.Wait()

	assert.False(t, shared1.isClosed(), "other shared publisher should not be closed")
}

func TestRoute_AddPublisher_SharedFail_SucceedsWithOtherShared(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	shared1 := newMockPublisher("shared1", PublishModeSharedFail)
	_, err := route.AddPublisher(ctx, shared1)
	require.NoError(t, err)

	shared2 := newMockPublisher("shared2", PublishModeSharedFail)
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		publishers, err2 := route.AddPublisherLocked(ctx, shared2, &wg)
		require.NoError(t, err2)
		require.Len(t, publishers, 2)
	})
	wg.Wait()
}

func TestRoute_AddPublisher_ClosedRoute(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Close the route node.
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		route.closeNodeLocked(ctx, &wg)
	})
	wg.Wait()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)

	var wg2 sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err := route.AddPublisherLocked(ctx, pub, &wg2)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrRouteClosed{}))
	})
	wg2.Wait()
}

func TestRoute_RemovePublisher_Success(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	publishers, err := route.RemovePublisher(ctx, pub)
	require.NoError(t, err)
	assert.Empty(t, publishers)
}

func TestRoute_RemovePublisher_NotFound(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.RemovePublisher(ctx, pub)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublisherNotFound{}))
}

func TestRoute_RemovePublisher_FromMultiple(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub1 := newMockPublisher("pub1", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	pub2 := newMockPublisher("pub2", PublishModeSharedTakeover)
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, pub2, &wg)
		require.NoError(t, err)
	})
	wg.Wait()

	publishers, err := route.RemovePublisher(ctx, pub1)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
	assert.Same(t, pub2, publishers[0])
}

func TestRoute_GetPublishers(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Initially empty.
	pubs := route.GetPublishers(ctx)
	assert.Empty(t, pubs)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	pubs = route.GetPublishers(ctx)
	require.Len(t, pubs, 1)
	assert.Same(t, pub, pubs[0])
}

func TestRoute_WaitForPublisher_AlreadyExists(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	publishers, err := route.WaitForPublisher(ctx)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
	assert.Same(t, pub, publishers[0])
}

func TestRoute_WaitForPublisher_Cancelled(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := route.WaitForPublisher(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRoute_WaitForPublisher_Async(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	var foundPubs Publishers[any]
	var foundErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		foundPubs, foundErr = route.WaitForPublisher(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForPublisher did not return in time")
	}

	require.NoError(t, foundErr)
	require.Len(t, foundPubs, 1)
	assert.Same(t, pub, foundPubs[0])
}

// NOTE: ResetNode tests are intentionally omitted because ResetNode has
// complex lifecycle interactions with the Router's WaitGroup accounting.
// openNodeLocked (called by ResetNode) triggers onRouteCreated which adds
// to the WaitGroup, but the corresponding Done only happens via RemoveRoute
// which isn't called for the extra Add. This is a known lifecycle complexity
// that makes unit testing ResetNode through the Router unreliable.

func TestRoute_AddPublisher_UnknownMode_ReturnsError(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// First add a publisher so that the mode-check branch with len(Publishers) > 0 is hit.
	existing := newMockPublisher("existing", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, existing)
	require.NoError(t, err)

	unknownPub := newMockPublisher("unknown", PublishMode(999))

	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, unknownPub, &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown publishing mode")
	})
	wg.Wait()
}

func TestRoute_OnPublisherAdded_Callback(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	var callCount atomic.Int32
	route.Locker().Do(ctx, func() {
		route.OnPublisherAdded = func(ctx context.Context, r *Route[any], p Publisher[any]) {
			callCount.Add(1)
		}
	})

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
}

func TestRoute_OnPublisherRemoved_Callback(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	var callCount atomic.Int32
	route.Locker().Do(ctx, func() {
		route.OnPublisherRemoved = func(ctx context.Context, r *Route[any], p Publisher[any]) {
			callCount.Add(1)
		}
	})

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	_, err = route.RemovePublisher(ctx, pub)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
}

func TestRoute_ConcurrentAddRemovePublisher(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	const iterations = 10
	var wg sync.WaitGroup
	for i := range iterations {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pub := newMockPublisher(fmt.Sprintf("pub-%d", i), PublishModeExclusiveTakeover)
			_, err := route.AddPublisher(ctx, pub)
			if err != nil {
				// Conflicts are expected in concurrent exclusive mode.
				return
			}
			// Small delay before removing.
			time.Sleep(time.Duration(i) * time.Millisecond)
			route.RemovePublisher(ctx, pub)
		}(i)
	}
	wg.Wait()
}

func TestPublishers_String_None(t *testing.T) {
	var pubs Publishers[any]
	assert.Equal(t, "NONE", pubs.String())
}

func TestPublishers_String_Single(t *testing.T) {
	pub := newMockPublisher("my-publisher", PublishModeExclusiveTakeover)
	pubs := Publishers[any]{pub}
	assert.Equal(t, "my-publisher", pubs.String())
}

func TestPublishers_String_Multiple(t *testing.T) {
	pub1 := newMockPublisher("pub-a", PublishModeSharedTakeover)
	pub2 := newMockPublisher("pub-b", PublishModeSharedTakeover)
	pubs := Publishers[any]{pub1, pub2}
	assert.Equal(t, "[pub-a,pub-b]", pubs.String())
}

func TestRoute_PublishersChangeChan_SignalsOnAdd(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Get the current change channel.
	ch := route.getPublishersChangeChan(ctx)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	select {
	case <-ch:
		// expected: channel was closed (signaling a change)
	case <-time.After(2 * time.Second):
		t.Fatal("publishers change channel was not signaled after add")
	}
}

func TestRoute_PublishersChangeChan_SignalsOnRemove(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// Get the current change channel after adding.
	ch := route.getPublishersChangeChan(ctx)

	_, err = route.RemovePublisher(ctx, pub)
	require.NoError(t, err)

	select {
	case <-ch:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("publishers change channel was not signaled after remove")
	}
}

func TestRoute_ExclusiveTakeover_ClosesOldPublisherWithError(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	closeErr := fmt.Errorf("intentional close error")
	pub1 := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	pub1.closeFn = func(ctx context.Context) error {
		return closeErr
	}
	_, err := route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	// The new publisher should still succeed even if old close returns error.
	pub2 := newMockPublisher("pub2", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub2)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.True(t, pub1.isClosed())
}

func TestRoute_Locker(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	locker := route.Locker()
	assert.NotNil(t, locker)
}

func TestRoute_LockDo(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	var executed bool
	route.LockDo(ctx, func(ctx context.Context) {
		executed = true
	})
	assert.True(t, executed)
}
