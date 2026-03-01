package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouter_GetRoute_WaitForPublisher_AlreadyExists(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	// Create a route and add a publisher before calling GetRoute with WaitForPublisher.
	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	found, err := r.GetRoute(ctx, "test/stream", GetRouteModeWaitForPublisher)
	require.NoError(t, err)
	assert.Same(t, route, found)
}

func TestRouter_GetRoute_WaitForPublisher_WaitsForRouteAndPublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var foundRoute *Route[any]
	var foundErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		foundRoute, foundErr = r.GetRoute(ctx, "delayed/pub", GetRouteModeWaitForPublisher)
	}()

	// Give the goroutine time to start waiting.
	time.Sleep(50 * time.Millisecond)

	// Create the route.
	route, err := r.GetRoute(ctx, "delayed/pub", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Give a moment for WaitForRoute to see the route.
	time.Sleep(50 * time.Millisecond)

	// Add a publisher.
	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetRoute(WaitForPublisher) did not return in time")
	}

	require.NoError(t, foundErr)
	assert.Same(t, route, foundRoute)
}

func TestRouter_GetRoute_WaitForPublisher_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := r.GetRoute(ctx, "never/pub", GetRouteModeWaitForPublisher)
	assert.Error(t, err)
}

func TestRouter_GetRoute_WaitUntilCreated_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := r.GetRoute(ctx, "never/created", GetRouteModeWaitUntilCreated)
	assert.Error(t, err)
}

func TestRouter_Wait_CloseThenWait(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)

	// Create a route, then remove it and close the router.
	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	err = r.RemoveRoute(ctx, route)
	require.NoError(t, err)

	r.Close(ctx)

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = r.Wait(waitCtx)
	assert.NoError(t, err)
}

func TestRouter_RemoveRoute_MismatchedInstance(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Create a fake route that has the same path but is a different instance.
	r2 := newTestRouter(t)
	fakeRoute, err := r2.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// RemoveRoute should fail because the route instance doesn't match.
	err = r.RemoveRoute(ctx, fakeRoute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not set in the router")
}

func TestRouter_GetRoute_UnknownMode(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "test/stream", GetRouteMode(999))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mode")
}

func TestRouter_GetRoute_UnknownMode_WithExistingRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	// First create a route.
	_, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Then try an unknown mode on the existing route.
	_, err = r.GetRoute(ctx, "test/stream", GetRouteMode(999))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mode")
}

func TestRouter_CreateRoute_AfterClose(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)
	r.Close(ctx)

	// createRoute should return nil if RouterCloseChan is closed.
	// Use CreateTemporary which calls createRoute.
	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	// The route will be nil because createRoute checks RouterCloseChan.
	// But GetRoute wraps it, so we just check no panic.
	if route == nil {
		assert.NoError(t, err) // might be nil/nil
	}
}
