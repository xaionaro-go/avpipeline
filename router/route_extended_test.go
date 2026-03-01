package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoute_Close_NilRoute(t *testing.T) {
	var route *Route[any]
	err := route.Close(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestRoute_CloseNodeLocked_AlreadyClosed(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Close the node once.
	var wg1 sync.WaitGroup
	route.Locker().Do(ctx, func() {
		err := route.closeNodeLocked(ctx, &wg1)
		assert.NoError(t, err)
	})
	wg1.Wait()

	// Close again - should return ErrAlreadyClosed.
	var wg2 sync.WaitGroup
	route.Locker().Do(ctx, func() {
		err := route.closeNodeLocked(ctx, &wg2)
		assert.ErrorIs(t, err, ErrAlreadyClosed{})
	})
	wg2.Wait()
}

func TestRoute_OpenNodeLocked_AlreadyOpen(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Wait for Serve goroutine to start, then cancel and wait for it to stop.
	waitForServing(t, route)
	route.CancelFunc()
	waitForNotServing(t, route)

	// Allow the deferred r.Close(ctx) in the newRoute goroutine to complete.
	time.Sleep(200 * time.Millisecond)

	// The node may have been closed by the deferred Close. Re-open it
	// under the lock so we can test the "already open" path.
	extraAdds := 0
	route.Locker().Do(ctx, func() {
		if !route.IsNodeOpen {
			route.openNodeLocked(ctx)
			extraAdds++
		}
		assert.True(t, route.IsNodeOpen)
		// This should log "node is already open" but not crash.
		route.openNodeLocked(ctx)
		assert.True(t, route.IsNodeOpen)
	})

	// Clean up: openNodeLocked calls OnOpen → onRouteCreated → WaitGroup.Add(1).
	// We need matching Done calls.
	for range extraAdds {
		r.WaitGroup.Done()
	}
	r.Locker.Do(ctx, func() {
		delete(r.RoutesByPath, route.Path)
	})
	r.Close(ctx)
}

func TestRoute_ResetNode(t *testing.T) {
	// NOTE: ResetNode has complex lifecycle interactions with the Router's WaitGroup.
	// openNodeLocked (called by ResetNode) triggers onRouteCreated which adds
	// to the WaitGroup. closeNodeLocked triggers onRouteClosed which calls
	// RemoveRoute. This all happens while holding the route's Node.Locker,
	// and the Serve goroutine may race with it. We test that ResetNode
	// completes without error, which covers resetNodeLocked, closeNodeLocked,
	// and openNodeLocked code paths.
	ctx := context.Background()
	r := New[any](ctx)

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	require.NotNil(t, route)

	// Verify node is open before reset.
	assert.True(t, route.IsOpen(ctx))

	// Wait for Serve goroutine to start, then cancel and wait for it to stop.
	waitForServing(t, route)
	route.CancelFunc()
	waitForNotServing(t, route)

	// Allow the deferred r.Close(ctx) in the newRoute goroutine to complete.
	time.Sleep(200 * time.Millisecond)

	// ResetNode closes the old node and opens a new one.
	err = route.ResetNode(ctx)
	// ResetNode should succeed (closeNodeLocked may return ErrAlreadyClosed
	// which is explicitly ignored in resetNodeLocked).
	assert.NoError(t, err)

	// Clean up. ResetNode opens a new node but does NOT start a new Serve
	// goroutine (only newRoute does). The WaitGroup has extra Adds from
	// openNodeLocked -> onRouteCreated. We need to drain them.
	r.WaitGroup.Done()
	r.Locker.Do(ctx, func() {
		delete(r.RoutesByPath, route.Path)
	})
	r.Close(ctx)
}

func TestRoute_ResetNode_AlreadyClosed(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Wait for Serve goroutine to start, then cancel and wait for it to stop.
	waitForServing(t, route)
	route.CancelFunc()
	waitForNotServing(t, route)

	// Allow the deferred r.Close(ctx) in the newRoute goroutine to complete.
	time.Sleep(200 * time.Millisecond)

	// Close the node first (may already be closed by deferred Close above).
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		route.closeNodeLocked(ctx, &wg)
	})
	wg.Wait()

	// ResetNode should handle the already-closed state gracefully
	// (closeNodeLocked returns ErrAlreadyClosed which is ignored).
	err = route.ResetNode(ctx)
	assert.NoError(t, err)

	// Node should be open again after reset.
	assert.True(t, route.IsOpen(ctx))

	// Clean up. ResetNode opens a new node but does NOT start a new Serve
	// goroutine (only newRoute does).
	r.WaitGroup.Done() // Extra from ResetNode's openNodeLocked
	r.RemoveRoute(ctx, route)
	r.Close(ctx)
}

func TestRoute_CloseLocked_AlreadyClosed(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// First close the node, then closeLocked should skip closeNodeLocked.
	var wg1 sync.WaitGroup
	route.Locker().Do(ctx, func() {
		route.closeNodeLocked(ctx, &wg1)
	})
	wg1.Wait()

	// closeLocked with IsNodeOpen=false should just call CancelFunc.
	var wg2 sync.WaitGroup
	route.Locker().Do(ctx, func() {
		err := route.closeLocked(ctx, &wg2)
		assert.NoError(t, err)
	})
	wg2.Wait()
}

func TestRoute_AddPublisher_ExclusiveTakeover_ReplacesMultipleShared(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Add multiple shared publishers.
	shared1 := newMockPublisher("shared1", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, shared1)
	require.NoError(t, err)

	shared2 := newMockPublisher("shared2", PublishModeSharedTakeover)
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, shared2, &wg)
		require.NoError(t, err)
	})
	wg.Wait()

	// Now add an exclusive takeover publisher - should remove all existing publishers.
	exclusive := newMockPublisher("exclusive", PublishModeExclusiveTakeover)
	publishers, err := route.AddPublisher(ctx, exclusive)
	require.NoError(t, err)
	require.Len(t, publishers, 1)
	assert.Same(t, exclusive, publishers[0])

	// Wait for async close of shared publishers.
	time.Sleep(100 * time.Millisecond)
	assert.True(t, shared1.isClosed(), "shared1 should have been closed")
	assert.True(t, shared2.isClosed(), "shared2 should have been closed")
}

func TestRoute_AddPublisher_ExclusiveFail_WithExistingShared(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	// Add a shared publisher.
	shared := newMockPublisher("shared", PublishModeSharedTakeover)
	_, err := route.AddPublisher(ctx, shared)
	require.NoError(t, err)

	// ExclusiveFail should fail because there's an existing publisher.
	excl := newMockPublisher("exclusive", PublishModeExclusiveFail)
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		_, err = route.AddPublisherLocked(ctx, excl, &wg)
		assert.ErrorIs(t, err, ErrAlreadyHasPublisher{})
	})
	wg.Wait()
}

func TestRoute_RemovePublisher_WhenNodeClosed(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")
	ctx := context.Background()

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err := route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// Close the node.
	var wg sync.WaitGroup
	route.Locker().Do(ctx, func() {
		route.closeNodeLocked(ctx, &wg)
	})
	wg.Wait()

	// RemovePublisher on a closed node should still find the publisher.
	publishers, err := route.RemovePublisher(ctx, pub)
	require.NoError(t, err)
	assert.Empty(t, publishers)
}

func TestRoute_ShouldFixPTS_AtomicBool(t *testing.T) {
	_, route := newTestRouteViaRouter(t, "test/stream")

	// Default should be false.
	assert.False(t, route.ShouldFixPTS.Load())

	route.ShouldFixPTS.Store(true)
	assert.True(t, route.ShouldFixPTS.Load())
}
