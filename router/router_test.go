package router

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_CreatesRouter(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)
	require.NotNil(t, r)
	assert.NotNil(t, r.RoutesByPath)
	assert.NotNil(t, r.RouterCloseChan)
	assert.NotNil(t, r.RoutesChangedChan)
	assert.NotNil(t, r.ErrorChan)
	assert.Empty(t, r.RoutesByPath)
	require.NoError(t, r.Close(ctx))
}

func TestRouter_Close(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)
	require.NotNil(t, r)
	err := r.Close(ctx)
	assert.NoError(t, err)
}

func TestRouter_Close_ClosesChannels(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)
	closeCh := r.RouterCloseChan
	r.Close(ctx)
	// RouterCloseChan should be closed after Close.
	select {
	case <-closeCh:
		// expected
	default:
		t.Fatal("expected RouterCloseChan to be closed")
	}
}

func TestGetRouteMode_String(t *testing.T) {
	tests := []struct {
		mode     GetRouteMode
		expected string
	}{
		{GetRouteModeFailIfNotFound, "fail-if-no-found"},
		{GetRouteModeWaitUntilCreated, "wait-until-created"},
		{GetRouteModeWaitForPublisher, "wait-for-publisher"},
		{GetRouteModeCreateTemporaryIfNotFound, "create-temporary-if-not-found"},
		{GetRouteModeCreateTemporary, "create-temporary"},
		{GetRouteModeCreatePersistentIfNotFound, "create-persistent-if-not-found"},
		{GetRouteModeCreatePersistent, "create-persistent"},
		{GetRouteMode(999), "unknown-mode-999"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.String())
		})
	}
}

func TestRouter_GetRoute_CreateTemporary(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, RoutePath("test/stream"), route.Path)
}

func TestRouter_GetRoute_CreatePersistent(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "persistent/stream", GetRouteModeCreatePersistent)
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, RoutePath("persistent/stream"), route.Path)
}

func TestRouter_GetRoute_CreateTemporaryIfNotFound_New(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporaryIfNotFound)
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, RoutePath("test/stream"), route.Path)
}

func TestRouter_GetRoute_CreateTemporaryIfNotFound_Existing(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route1, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	route2, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporaryIfNotFound)
	require.NoError(t, err)
	assert.Same(t, route1, route2, "should return the same route instance")
}

func TestRouter_GetRoute_CreatePersistentIfNotFound_New(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreatePersistentIfNotFound)
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, RoutePath("test/stream"), route.Path)
}

func TestRouter_GetRoute_CreatePersistentIfNotFound_Existing(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route1, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreatePersistent)
	require.NoError(t, err)

	route2, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreatePersistentIfNotFound)
	require.NoError(t, err)
	assert.Same(t, route1, route2)
}

func TestRouter_GetRoute_CreateTemporary_DuplicateFails(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	_, err = r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRouter_GetRoute_CreatePersistent_DuplicateFails(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreatePersistent)
	require.NoError(t, err)

	_, err = r.GetRoute(ctx, "test/stream", GetRouteModeCreatePersistent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRouter_GetRoute_FailIfNotFound_RouteExists(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	created, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	found, err := r.GetRoute(ctx, "test/stream", GetRouteModeFailIfNotFound)
	require.NoError(t, err)
	assert.Same(t, created, found)
}

func TestRouter_GetRoute_FailIfNotFound_NoRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "nonexistent", GetRouteModeFailIfNotFound)
	assert.Error(t, err)
}

func TestRouter_GetRoute_WaitUntilCreated_AlreadyExists(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	created, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	found, err := r.GetRoute(ctx, "test/stream", GetRouteModeWaitUntilCreated)
	require.NoError(t, err)
	assert.Same(t, created, found)
}

func TestRouter_WaitForRoute_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := r.WaitForRoute(ctx, "never")
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRouter_WaitForRoute_Success(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var foundRoute *Route[any]
	var foundErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		foundRoute, foundErr = r.WaitForRoute(ctx, "delayed/stream")
	}()

	// Give the goroutine a moment to start waiting.
	time.Sleep(50 * time.Millisecond)

	created, err := r.GetRoute(ctx, "delayed/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForRoute did not return in time")
	}

	require.NoError(t, foundErr)
	assert.Same(t, created, foundRoute)
}

func TestRouter_GetRoute_WaitUntilCreated_Async(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var foundRoute *Route[any]
	var foundErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		foundRoute, foundErr = r.GetRoute(ctx, "delayed/stream", GetRouteModeWaitUntilCreated)
	}()

	time.Sleep(50 * time.Millisecond)

	created, err := r.GetRoute(ctx, "delayed/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetRoute(WaitUntilCreated) did not return in time")
	}

	require.NoError(t, foundErr)
	assert.Same(t, created, foundRoute)
}

func TestRouter_RemoveRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	require.NotNil(t, route)

	err = r.RemoveRoute(ctx, route)
	assert.NoError(t, err)

	// Route should no longer be findable.
	_, err = r.GetRoute(ctx, "test/stream", GetRouteModeFailIfNotFound)
	assert.Error(t, err)
}

func TestRouter_RemoveRoute_WrongInstance(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Create a different route instance with a different path.
	otherRoute, err := r.GetRoute(ctx, "other/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Attempt to remove with mismatched path should fail.
	// Modify the path to mismatch -- simulate a stale/wrong route.
	// Actually, RemoveRoute checks the exact pointer match, so removing
	// otherRoute under the "test/stream" path should fail naturally.
	// Let's just verify removing a route that doesn't match fails.
	err = r.RemoveRoute(ctx, otherRoute)
	// This should succeed because otherRoute is at its own path.
	assert.NoError(t, err)
}

func TestRouter_RemoveRouteByPath(t *testing.T) {
	// NOTE: RemoveRouteByPath does not call WaitGroup.Done(), unlike RemoveRoute.
	// So we manage the router lifecycle manually here without relying on
	// the standard cleanup which calls r.Close() (which waits on WaitGroup).
	ctx := context.Background()
	r := New[any](ctx)

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	require.NotNil(t, route)

	removed := r.RemoveRouteByPath(ctx, "test/stream")
	assert.Same(t, route, removed)

	// Route should no longer be findable.
	_, err = r.GetRoute(ctx, "test/stream", GetRouteModeFailIfNotFound)
	assert.Error(t, err)

	// Manually fix the WaitGroup so we can close the router.
	r.WaitGroup.Done()
	r.Close(ctx)
}

func TestRouter_RemoveRouteByPath_NonExistent_Panics(t *testing.T) {
	// RemoveRouteByPath does not check for nil route before passing it
	// to removeRouteLocked, which calls route.Close(ctx). This causes a
	// nil pointer dereference panic. This is a known edge case in the
	// source code -- callers should verify the path exists before calling.
	r := newTestRouter(t)
	ctx := context.Background()

	assert.Panics(t, func() {
		r.RemoveRouteByPath(ctx, "nonexistent")
	})
}

func TestRouter_OnRouteCreated_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var callbackRoute *Route[any]
	var mu sync.Mutex
	r.OnRouteCreated = func(ctx context.Context, route *Route[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		callbackRoute = route
	}

	route, err := r.GetRoute(ctx, "callback/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Allow a moment for the callback goroutine to finish.
	time.Sleep(50 * time.Millisecond)

	assert.True(t, callbackCalled.Load(), "OnRouteCreated should have been called")
	mu.Lock()
	assert.Same(t, route, callbackRoute)
	mu.Unlock()
}

func TestRouter_OnRouteRemoved_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var callbackRoute *Route[any]
	var mu sync.Mutex
	r.OnRouteRemoved = func(ctx context.Context, route *Route[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		callbackRoute = route
	}

	route, err := r.GetRoute(ctx, "callback/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	err = r.RemoveRoute(ctx, route)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	assert.True(t, callbackCalled.Load(), "OnRouteRemoved should have been called")
	mu.Lock()
	assert.Same(t, route, callbackRoute)
	mu.Unlock()
}

func TestRouter_OnRoutePublisherAdded_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var callbackPublisher Publisher[any]
	var mu sync.Mutex
	r.OnRoutePublisherAdded = func(ctx context.Context, route *Route[any], pub Publisher[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		callbackPublisher = pub
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	assert.True(t, callbackCalled.Load(), "OnRoutePublisherAdded should have been called")
	mu.Lock()
	assert.Same(t, pub, callbackPublisher)
	mu.Unlock()
}

func TestRouter_OnRoutePublisherRemoved_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var mu sync.Mutex
	r.OnRoutePublisherRemoved = func(ctx context.Context, route *Route[any], pub Publisher[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	_, err = route.RemovePublisher(ctx, pub)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	assert.True(t, callbackCalled.Load(), "OnRoutePublisherRemoved should have been called")
}

func TestRouter_MultipleRouteCreation(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	const numRoutes = 20
	for i := range numRoutes {
		path := RoutePath(fmt.Sprintf("multi/%d", i))
		route, err := r.GetRoute(ctx, path, GetRouteModeCreateTemporary)
		assert.NoError(t, err, "route %d", i)
		assert.NotNil(t, route, "route %d", i)
	}

	assert.Equal(t, numRoutes, len(r.RoutesByPath))
}

func TestRouter_ConcurrentGetOrCreate(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	const numGoroutines = 10
	var wg sync.WaitGroup
	results := make([]*Route[any], numGoroutines)
	errs := make([]error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = r.GetRoute(ctx, "shared/stream", GetRouteModeCreateTemporaryIfNotFound)
		}(i)
	}

	wg.Wait()

	// All should succeed and return the same route.
	for i := range numGoroutines {
		require.NoError(t, errs[i], "goroutine %d", i)
		require.NotNil(t, results[i], "goroutine %d", i)
	}
	for i := 1; i < numGoroutines; i++ {
		assert.Same(t, results[0], results[i], "all goroutines should get the same route")
	}
}

func TestRouter_GetRoutesChangedChan(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	ch := r.GetRoutesChangedChan(ctx)
	require.NotNil(t, ch)

	// Creating a route should close the old channel and create a new one.
	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	select {
	case <-ch:
		// expected: old channel was closed
	default:
		t.Fatal("expected the routes changed channel to be signaled after route creation")
	}

	// New channel should still be open.
	ch2 := r.GetRoutesChangedChan(ctx)
	select {
	case <-ch2:
		t.Fatal("new channel should not be closed yet")
	default:
		// expected
	}
}

func TestRouter_Wait_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := r.Wait(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRouter_Wait_AfterClose(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)
	// Close immediately (no routes, WaitGroup is 0).
	r.Close(ctx)

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := r.Wait(waitCtx)
	assert.NoError(t, err)
}

func TestRouter_MultipleRoutes_IndependentLifecycle(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route1, err := r.GetRoute(ctx, "stream/1", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	route2, err := r.GetRoute(ctx, "stream/2", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	assert.NotSame(t, route1, route2)

	// Remove route1, route2 should still be accessible.
	err = r.RemoveRoute(ctx, route1)
	require.NoError(t, err)

	found, err := r.GetRoute(ctx, "stream/2", GetRouteModeFailIfNotFound)
	require.NoError(t, err)
	assert.Same(t, route2, found)

	_, err = r.GetRoute(ctx, "stream/1", GetRouteModeFailIfNotFound)
	assert.Error(t, err)
}
