// router_consumer_callback_test.go pins the per-route and Router-level
// OnRouteConsumerAdded/Removed hooks. These hooks fire SYNCHRONOUSLY
// inside (*Route).AddConsumer / RemoveConsumer (see route.go:379 and
// route.go:423: the callback variable is read under the route's locker
// and invoked on the calling goroutine before the public method
// returns), so these tests do NOT need any time-based synchronization
// to observe the side-effect — it is already complete when AddConsumer
// returns.
//
// Live use of these hooks in production: avd's commandHandler in
// pkg/configapplier/command_handler.go installs OnRouteConsumerAdded /
// OnRouteConsumerRemoved to launch / stop the per-endpoint
// OnConsumerAdded / OnConsumerRemoved config commands declared in
// avd.conf (Endpoint.OnConsumerAdded / OnConsumerRemoved). The
// resolver retains pattern-materialized routes for the resolver's
// lifetime; see pkg/configapplier/endpoint_resolver.go's Release
// doc-comment. Today's load-bearing consumer is the config-command
// launcher above; do not delete the hooks.

package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouter_OnRouteConsumerAdded_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var callbackRoute *Route[any]
	var callbackConsumer Consumer[any]
	var mu sync.Mutex
	r.OnRouteConsumerAdded = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		callbackRoute = route
		callbackConsumer = cons
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cons := newMockConsumer("cons1")
	_, err = route.AddConsumer(ctx, cons)
	require.NoError(t, err)
	// Synchronous callback: assertion runs immediately after
	// AddConsumer returns; no Sleep needed (and no Sleep wanted —
	// race-detector noise is the only thing it added).

	assert.True(t, callbackCalled.Load(), "OnRouteConsumerAdded should have been called")
	mu.Lock()
	assert.Same(t, route, callbackRoute)
	assert.Same(t, cons, callbackConsumer)
	mu.Unlock()
}

func TestRouter_OnRouteConsumerRemoved_Callback(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var callbackCalled atomic.Bool
	var callbackRoute *Route[any]
	var callbackConsumer Consumer[any]
	var mu sync.Mutex
	r.OnRouteConsumerRemoved = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		callbackRoute = route
		callbackConsumer = cons
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cons := newMockConsumer("cons1")
	_, err = route.AddConsumer(ctx, cons)
	require.NoError(t, err)

	_, err = route.RemoveConsumer(ctx, cons)
	require.NoError(t, err)

	// Synchronous callback (see file-level doc-comment); no Sleep.
	assert.True(t, callbackCalled.Load(), "OnRouteConsumerRemoved should have been called")
	mu.Lock()
	assert.Same(t, route, callbackRoute)
	assert.Same(t, cons, callbackConsumer)
	mu.Unlock()
}

func TestRouter_OnRouteConsumer_AddRemoveAddSequence(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	type event struct {
		kind string // "add" or "remove"
		cons Consumer[any]
	}

	var mu sync.Mutex
	var events []event
	r.OnRouteConsumerAdded = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event{kind: "add", cons: cons})
	}
	r.OnRouteConsumerRemoved = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event{kind: "remove", cons: cons})
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cons1 := newMockConsumer("cons1")
	_, err = route.AddConsumer(ctx, cons1)
	require.NoError(t, err)

	_, err = route.RemoveConsumer(ctx, cons1)
	require.NoError(t, err)

	cons2 := newMockConsumer("cons2")
	_, err = route.AddConsumer(ctx, cons2)
	require.NoError(t, err)

	// Synchronous callback ordering: events slice is fully populated
	// by the time AddConsumer/RemoveConsumer return. No Sleep needed.
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 3, "should have observed exactly add+remove+add")
	assert.Equal(t, "add", events[0].kind)
	assert.Same(t, cons1, events[0].cons)
	assert.Equal(t, "remove", events[1].kind)
	assert.Same(t, cons1, events[1].cons)
	assert.Equal(t, "add", events[2].kind)
	assert.Same(t, cons2, events[2].cons)
}

func TestRouter_OnRouteConsumer_NilCallbacksSafe(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()
	// Explicitly leave OnRouteConsumerAdded/Removed as nil.

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cons := newMockConsumer("cons1")
	// Add and remove should not panic when router-level callbacks are nil.
	assert.NotPanics(t, func() {
		_, err := route.AddConsumer(ctx, cons)
		require.NoError(t, err)
		_, err = route.RemoveConsumer(ctx, cons)
		require.NoError(t, err)
	})
}

func TestRouter_OnRouteConsumer_PerRouteHooksStillFire(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	var routerAddCount, routerRemoveCount atomic.Int64
	r.OnRouteConsumerAdded = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		routerAddCount.Add(1)
	}
	r.OnRouteConsumerRemoved = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		routerRemoveCount.Add(1)
	}

	route, err := r.GetRoute(ctx, "test/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// Save the existing per-route hooks (set by Router) and chain user hooks
	// on top, ensuring both still fire.
	routerLevelAdd := route.OnConsumerAdded
	routerLevelRemove := route.OnConsumerRemoved

	var perRouteAddCount, perRouteRemoveCount atomic.Int64
	route.OnConsumerAdded = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		perRouteAddCount.Add(1)
		if routerLevelAdd != nil {
			routerLevelAdd(ctx, route, cons)
		}
	}
	route.OnConsumerRemoved = func(ctx context.Context, route *Route[any], cons Consumer[any]) {
		perRouteRemoveCount.Add(1)
		if routerLevelRemove != nil {
			routerLevelRemove(ctx, route, cons)
		}
	}

	cons := newMockConsumer("cons1")
	_, err = route.AddConsumer(ctx, cons)
	require.NoError(t, err)
	_, err = route.RemoveConsumer(ctx, cons)
	require.NoError(t, err)

	// Synchronous callback chain: counters are final on return.
	assert.Equal(t, int64(1), perRouteAddCount.Load(), "per-route OnConsumerAdded should have fired once")
	assert.Equal(t, int64(1), perRouteRemoveCount.Load(), "per-route OnConsumerRemoved should have fired once")
	assert.Equal(t, int64(1), routerAddCount.Load(), "router-level OnRouteConsumerAdded should have fired once")
	assert.Equal(t, int64(1), routerRemoveCount.Load(), "router-level OnRouteConsumerRemoved should have fired once")
}
