package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests simulate the actual usage patterns of avd (LibAV Daemon)
// to ensure avpipeline's router never creates problems for avd.

// TestAVD_PublisherConnectConsumerWait simulates avd's core flow:
// 1. Consumer connects first (waits for publisher)
// 2. Publisher connects and creates route source
// 3. Consumer detects publisher and starts forwarding
// 4. Publisher disconnects
func TestAVD_PublisherConnectConsumerWait(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	type result struct {
		route *Route[any]
		err   error
	}
	resultCh := make(chan result, 1)

	// Step 1: Consumer tries to get route, waits for publisher
	go func() {
		route, err := r.GetRoute(ctx, routePath, GetRouteModeWaitForPublisher)
		resultCh <- result{route, err}
	}()
	time.Sleep(50 * time.Millisecond)

	// Step 2: Publisher creates route
	pubRoute, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)
	require.NotNil(t, pubRoute)

	// Step 3: Add publisher to trigger consumer notification
	pub := newMockPublisher("rtmp-publisher", PublishModeExclusiveTakeover)
	_, err = pubRoute.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// Consumer should eventually get the route
	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		require.NotNil(t, res.route)
		assert.Equal(t, routePath, res.route.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for consumer to get route")
	}
}

// TestAVD_MultipleConsumersSameRoute simulates multiple avd consumers
// connecting to the same route (e.g., multiple RTMP clients watching same stream).
func TestAVD_MultipleConsumersSameRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	// Create route and add publisher
	route, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("publisher", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// Multiple consumers get the same route
	const numConsumers = 5
	var wg sync.WaitGroup
	routes := make([]*Route[any], numConsumers)
	errors := make([]error, numConsumers)

	wg.Add(numConsumers)
	for i := 0; i < numConsumers; i++ {
		go func(idx int) {
			defer wg.Done()
			routes[idx], errors[idx] = r.GetRoute(ctx, routePath, GetRouteModeFailIfNotFound)
		}(i)
	}
	wg.Wait()

	for i := 0; i < numConsumers; i++ {
		require.NoError(t, errors[i], "consumer %d", i)
		require.NotNil(t, routes[i], "consumer %d", i)
		assert.Same(t, route, routes[i], "all consumers should get same route")
	}
}

// TestAVD_PublisherDisconnectReconnect simulates publisher disconnect and reconnect
// (common with RTMP publishers dropping and reconnecting).
func TestAVD_PublisherDisconnectReconnect(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	// First publisher connects
	route, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub1 := newMockPublisher("publisher-1", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	// Verify publisher is registered
	publishers := route.GetPublishers(ctx)
	assert.Len(t, publishers, 1)

	// Publisher disconnects
	_, err = route.RemovePublisher(ctx, pub1)
	require.NoError(t, err)

	publishers = route.GetPublishers(ctx)
	assert.Len(t, publishers, 0)

	// New publisher reconnects to same route
	pub2 := newMockPublisher("publisher-2", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub2)
	require.NoError(t, err)

	publishers = route.GetPublishers(ctx)
	assert.Len(t, publishers, 1)
}

// TestAVD_ExclusiveTakeoverReplacesPublisher simulates avd's publisher replacement
// when a new RTMP publisher connects with ExclusiveTakeover mode.
func TestAVD_ExclusiveTakeoverReplacesPublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	route, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// First publisher
	pub1 := newMockPublisher("old-publisher", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	// New publisher with ExclusiveTakeover should replace old one
	pub2 := newMockPublisher("new-publisher", PublishModeExclusiveTakeover)
	pubs, err := route.AddPublisher(ctx, pub2)
	require.NoError(t, err)
	assert.Len(t, pubs, 1)

	// Old publisher should have been closed
	time.Sleep(50 * time.Millisecond) // close happens async
	assert.True(t, pub1.isClosed(), "old publisher should be closed")
}

// TestAVD_ExclusiveFailRejectsSecondPublisher simulates avd rejecting
// a second publisher when mode is ExclusiveFail.
func TestAVD_ExclusiveFailRejectsSecondPublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	route, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub1 := newMockPublisher("publisher-1", PublishModeExclusiveFail)
	_, err = route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	pub2 := newMockPublisher("publisher-2", PublishModeExclusiveFail)
	_, err = route.AddPublisher(ctx, pub2)
	assert.Error(t, err)
	assert.IsType(t, ErrAlreadyHasPublisher{}, err)
}

// TestAVD_WaitForPublisherTimeout simulates consumer timeout when no publisher appears.
func TestAVD_WaitForPublisherTimeout(t *testing.T) {
	r := newTestRouter(t)
	routePath := RoutePath("live/nonexistent")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := r.GetRoute(ctx, routePath, GetRouteModeWaitForPublisher)
	assert.Error(t, err) // should timeout
}

// TestAVD_RouteCallbacksOnOpenClose simulates avd's OnRouteCreated/OnRouteRemoved callbacks.
func TestAVD_RouteCallbacksOnOpenClose(t *testing.T) {
	ctx := context.Background()
	r := New[any](ctx)

	var mu sync.Mutex
	var created, removed []RoutePath

	r.OnRouteCreated = func(ctx context.Context, route *Route[any]) {
		mu.Lock()
		defer mu.Unlock()
		created = append(created, route.Path)
	}
	r.OnRouteRemoved = func(ctx context.Context, route *Route[any]) {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, route.Path)
	}

	route, err := r.GetRoute(ctx, "live/test", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	mu.Lock()
	assert.Contains(t, created, RoutePath("live/test"))
	mu.Unlock()

	r.RemoveRoute(ctx, route)

	// Allow deferred Close to run
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Contains(t, removed, RoutePath("live/test"))
	mu.Unlock()

	// Cleanup
	r.Close(ctx)
}

// TestAVD_ConcurrentPublisherConsumerOperations simulates the real-world scenario
// where publishers and consumers operate concurrently on the router.
func TestAVD_ConcurrentPublisherConsumerOperations(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	const numStreams = 3
	var wg sync.WaitGroup

	// Simulate multiple streams being created and accessed concurrently
	for i := 0; i < numStreams; i++ {
		wg.Add(2)
		path := RoutePath("live/stream" + string(rune('0'+i)))

		// Publisher goroutine
		go func(p RoutePath) {
			defer wg.Done()
			route, err := r.GetRoute(ctx, p, GetRouteModeCreateTemporary)
			if err != nil {
				return
			}
			pub := newMockPublisher("pub-"+string(p), PublishModeExclusiveTakeover)
			route.AddPublisher(ctx, pub)
			time.Sleep(50 * time.Millisecond)
			route.RemovePublisher(ctx, pub)
		}(path)

		// Consumer goroutine
		go func(p RoutePath) {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // slight delay
			_, _ = r.GetRoute(ctx, p, GetRouteModeFailIfNotFound)
		}(path)
	}

	wg.Wait()
}

// TestAVD_WaitForPublisherOnRoute simulates avd consumer waiting
// for a publisher on an existing route using WaitForPublisher.
func TestAVD_WaitForPublisherOnRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	routePath := RoutePath("live/stream1")

	// Create route first
	route, err := r.GetRoute(ctx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	type result struct {
		pubs Publishers[any]
		err  error
	}
	resultCh := make(chan result, 1)

	go func() {
		pubs, err := route.WaitForPublisher(ctx)
		resultCh <- result{pubs, err}
	}()
	time.Sleep(50 * time.Millisecond)

	// Add publisher - should wake up the waiter
	pub := newMockPublisher("publisher", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// Wait for the waiter to complete
	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		require.Len(t, res.pubs, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WaitForPublisher to return")
	}
}

// TestAVD_WaitForPublisherCancellation simulates context cancellation
// while a consumer is waiting for a publisher.
func TestAVD_WaitForPublisherCancellation(t *testing.T) {
	r := newTestRouter(t)
	routePath := RoutePath("live/stream1")

	bgCtx := context.Background()
	route, err := r.GetRoute(bgCtx, routePath, GetRouteModeCreateTemporary)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(bgCtx, 100*time.Millisecond)
	defer cancel()

	_, err = route.WaitForPublisher(ctx)
	assert.Error(t, err)
}

// TestAVD_SharedPublishersCoexist simulates shared publish mode
// where multiple publishers can exist on the same route.
func TestAVD_SharedPublishersCoexist(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "live/shared", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub1 := newMockPublisher("shared-pub-1", PublishModeSharedFail)
	_, err = route.AddPublisher(ctx, pub1)
	require.NoError(t, err)

	pub2 := newMockPublisher("shared-pub-2", PublishModeSharedFail)
	_, err = route.AddPublisher(ctx, pub2)
	require.NoError(t, err)

	publishers := route.GetPublishers(ctx)
	assert.Len(t, publishers, 2)
}

// TestAVD_RouteIsOpen_LifecycleCheck checks route open/close state
// which avd checks before forwarding data.
func TestAVD_RouteIsOpen_LifecycleCheck(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "live/test", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	assert.True(t, route.IsOpen(ctx))

	// Close the route
	err = route.Close(ctx)
	require.NoError(t, err)

	assert.False(t, route.IsOpen(ctx))
}

// TestAVD_RouteSourceFullLifecycle simulates the complete lifecycle
// that avd's ConnectionProxiedHandlerPublisher goes through:
// AddRouteSource → Start → (stream data) → Stop → Close
func TestAVD_RouteSourceFullLifecycle(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	// Create source (simulating the input node)
	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	var lifecycleOrder []string
	var mu sync.Mutex
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		lifecycleOrder = append(lifecycleOrder, event)
	}

	// This is the exact pattern avd uses in StartForwarding
	fwd, err := AddRouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, r, srcRoute.Node, "live/dest", PublishModeExclusiveTakeover,
		nil, nil,
		func(_ context.Context, _ *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			record("post-start")
		},
		func(_ context.Context, _ *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			record("pre-stop")
		},
		func(_ context.Context, _ *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			record("post-stop")
		},
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	mu.Lock()
	assert.Contains(t, lifecycleOrder, "post-start")
	mu.Unlock()

	// Stop (simulating publisher disconnect)
	err = fwd.Stop(ctx)
	require.NoError(t, err)

	mu.Lock()
	assert.Contains(t, lifecycleOrder, "pre-stop")
	assert.Contains(t, lifecycleOrder, "post-stop")
	mu.Unlock()

	// Close (cleanup)
	err = fwd.Close(ctx)
	require.NoError(t, err)
}

// TestAVD_GetRoute_FailIfNotFound ensures that GetRoute returns error
// when route doesn't exist and mode is FailIfNotFound.
func TestAVD_GetRoute_FailIfNotFound(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "nonexistent/stream", GetRouteModeFailIfNotFound)
	assert.Error(t, err)
}

// TestAVD_GetRoute_CreatePersistentIfNotFound tests persistent route creation
// with "if not found" semantics (avd uses this for always-available routes).
func TestAVD_GetRoute_CreatePersistentIfNotFound(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route1, err := r.GetRoute(ctx, "persistent/stream", GetRouteModeCreatePersistentIfNotFound)
	require.NoError(t, err)

	// Getting same path with IfNotFound should return existing route
	route2, err := r.GetRoute(ctx, "persistent/stream", GetRouteModeCreatePersistentIfNotFound)
	require.NoError(t, err)
	assert.Same(t, route1, route2)
}

// TestAVD_GetRoute_CreatePersistent_DuplicateFails tests that creating
// a persistent route twice with "must create" mode fails.
func TestAVD_GetRoute_CreatePersistent_DuplicateFails(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	_, err := r.GetRoute(ctx, "persistent/stream", GetRouteModeCreatePersistent)
	require.NoError(t, err)

	_, err = r.GetRoute(ctx, "persistent/stream", GetRouteModeCreatePersistent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// TestAVD_PublisherAddToClosedRoute ensures proper error when
// adding a publisher to a closed route (race condition protection).
func TestAVD_PublisherAddToClosedRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "live/test", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Close the route
	err = route.Close(ctx)
	require.NoError(t, err)

	// Try to add publisher to closed route
	pub := newMockPublisher("pub", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	assert.Error(t, err)
}

// TestAVD_NilPublisher ensures proper error handling when nil publisher is added.
func TestAVD_NilPublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "live/test", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	_, err = route.AddPublisher(ctx, nil)
	assert.Error(t, err)
}

// TestAVD_DuplicatePublisher ensures proper error when same publisher added twice.
func TestAVD_DuplicatePublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "live/test", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("pub", PublishModeSharedFail)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	_, err = route.AddPublisher(ctx, pub)
	assert.Error(t, err)
	assert.IsType(t, ErrAlreadyAPublisher{}, err)
}
