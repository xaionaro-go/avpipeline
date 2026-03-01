package router

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteSource_AddRouteSource(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	// Create a source route and a source node.
	srcRoute, err := r.GetRoute(ctx, "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := AddRouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, r, srcRoute.Node, "dest/stream", PublishModeExclusiveTakeover,
		nil, nil, nil, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	assert.Equal(t, RoutePath("dest/stream"), fwd.DstPath)
	assert.Equal(t, PublishModeExclusiveTakeover, fwd.GetPublishMode(ctx))

	// Source should be the src node.
	assert.Same(t, srcRoute.Node, fwd.GetInputNode(ctx))

	// String should contain info.
	s := fwd.String()
	assert.Contains(t, s, "fwd(")

	// GetOutputRoute should return the dest route.
	outputRoute := fwd.GetOutputRoute(ctx)
	assert.NotNil(t, outputRoute)

	// Clean up.
	err = fwd.Close(ctx)
	assert.NoError(t, err)
}

func TestRouteSource_GetPublishMode(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		PublishMode: PublishModeSharedFail,
	}
	assert.Equal(t, PublishModeSharedFail, fwd.GetPublishMode(ctx))
}

func TestRouteSource_OpenLocked_AlreadyStarted(t *testing.T) {
	ctx := context.Background()
	r := newTestRouter(t)

	srcRoute, err := r.GetRoute(ctx, "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		Router:      r,
		Input:       srcRoute.Node,
		DstPath:     "dest/stream",
		PublishMode: PublishModeExclusiveTakeover,
		CancelFunc:  func() {},
	}

	err = fwd.open(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestRouteSource_StartAndStopCallbacks(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	var postStartCalled, preStopCalled, postStopCalled bool
	var mu sync.Mutex

	fwd, err := AddRouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, r, srcRoute.Node, "dest/stream", PublishModeExclusiveTakeover,
		nil, nil,
		func(ctx context.Context, rs *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			mu.Lock()
			defer mu.Unlock()
			postStartCalled = true
		},
		func(ctx context.Context, rs *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			mu.Lock()
			defer mu.Unlock()
			preStopCalled = true
		},
		func(ctx context.Context, rs *RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]) {
			mu.Lock()
			defer mu.Unlock()
			postStopCalled = true
		},
	)
	require.NoError(t, err)

	mu.Lock()
	assert.True(t, postStartCalled, "OnPostStart should have been called")
	mu.Unlock()

	// Stop the route source.
	err = fwd.Stop(ctx)
	assert.NoError(t, err)

	mu.Lock()
	assert.True(t, preStopCalled, "OnPreStop should have been called")
	assert.True(t, postStopCalled, "OnPostStop should have been called")
	mu.Unlock()

	// Close.
	err = fwd.Close(ctx)
	assert.NoError(t, err)
}

func TestRouteSource_Close_NilCancelFunc(t *testing.T) {
	ctx := context.Background()
	r := newTestRouter(t)

	srcRoute, err := r.GetRoute(ctx, "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		Router:      r,
		Input:       srcRoute.Node,
		DstPath:     "dest/stream",
		PublishMode: PublishModeExclusiveTakeover,
	}

	// Close with nil CancelFunc should not error.
	err = fwd.Close(ctx)
	assert.NoError(t, err)
}

func TestRouteSource_GetOutputRoute_NilOutput(t *testing.T) {
	ctx := context.Background()
	r := newTestRouter(t)

	srcRoute, err := r.GetRoute(ctx, "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		Router:      r,
		Input:       srcRoute.Node,
		DstPath:     "dest/stream",
		PublishMode: PublishModeExclusiveTakeover,
	}

	result := fwd.GetOutputRoute(ctx)
	assert.Nil(t, result)
}

func TestRouteSource_StartLocked_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srcRoute, err := r.GetRoute(context.Background(), "source/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		Router:      r,
		Input:       srcRoute.Node,
		DstPath:     "dest/stream",
		PublishMode: PublishModeExclusiveTakeover,
		CancelFunc:  func() {},
	}

	err = fwd.Start(ctx)
	assert.Error(t, err)
}
