package router

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteForwarding_String_NilInputNilOutput(t *testing.T) {
	fwd := &RouteForwarding[any]{}
	assert.Equal(t, "fwd(?->?)", fwd.String())
}

func TestRouteForwarding_String_WithInput(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/input", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{
		Input: route,
	}
	assert.Equal(t, "fwd('test/input'->?)", fwd.String())
}

func TestRouteForwarding_GetPublishMode(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{
		PublishMode: PublishModeExclusiveTakeover,
	}
	assert.Equal(t, PublishModeExclusiveTakeover, fwd.GetPublishMode(ctx))
}

func TestRouteForwarding_Close_NilCancelFunc(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{}
	// doCloseLocked returns nil when CancelFunc is nil.
	err := fwd.Close(ctx)
	assert.NoError(t, err)
}

func TestRouteForwarding_GetOutputRoute_NilOutput(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{}
	result := fwd.GetOutputRoute(ctx)
	assert.Nil(t, result)
}

func TestRouteForwarding_StopLocked_NilStreamForwarderAndOutput(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{}
	// stopLocked with nil StreamForwarder and nil Output should not panic.
	fwd.Locker.Do(ctx, func() {
		var wg sync.WaitGroup
		err := fwd.stopLocked(ctx, &wg)
		assert.NoError(t, err)
	})
}

func TestForwardOutputFactoryLocalPath_String(t *testing.T) {
	r := newTestRouter(t)
	factory := newForwardOutputFactoryLocalPath[any](r, "test/path")
	assert.Equal(t, "test/path", factory.String())
}

func TestRouteForwarding_Open_AlreadyStarted(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{}
	cancelFn := func() {}
	fwd.CancelFunc = cancelFn

	// openLocked should fail with "already started".
	err := fwd.open(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestRouteForwarding_String_WithOutput(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	inputRoute, err := r.GetRoute(ctx, "test/input", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	outputRoute, err := r.GetRoute(ctx, "test/output", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{
		Input:  inputRoute,
		Output: &forwardOutputNodeLocalPath[any]{NodeRouting: outputRoute.Node, RouteForwarding: nil},
	}
	s := fwd.String()
	assert.Contains(t, s, "test/input")
}

func TestRouteForwarding_String_OutputOnly(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	outputRoute, err := r.GetRoute(ctx, "test/output", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{
		Output: &forwardOutputNodeLocalPath[any]{NodeRouting: outputRoute.Node, RouteForwarding: nil},
	}
	s := fwd.String()
	assert.Contains(t, s, "fwd(?->")
}

func TestRouteForwarding_DoCloseLocked_WithCancelFunc(t *testing.T) {
	ctx := context.Background()

	var cancelled bool
	fwd := &RouteForwarding[any]{
		CancelFunc: func() { cancelled = true },
	}

	err := fwd.Close(ctx)
	assert.NoError(t, err)
	assert.True(t, cancelled)
}

func TestRouteForwarding_Stop_NilForwarderAndOutput(t *testing.T) {
	ctx := context.Background()
	fwd := &RouteForwarding[any]{}

	// stop with nil StreamForwarder and nil Output should not error.
	err := fwd.stop(ctx)
	assert.NoError(t, err)
}

func TestForwardOutputNodeLocalPath_String(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/path", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	n := &forwardOutputNodeLocalPath[any]{
		NodeRouting: route.Node,
	}
	assert.Equal(t, "test/path", n.String())
}

func TestForwardOutputNodeLocalPath_GetOutputRoute(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/path", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	n := &forwardOutputNodeLocalPath[any]{
		NodeRouting: route.Node,
	}
	result := n.GetOutputRoute(ctx)
	assert.Same(t, route, result)
}

func TestAddRouteForwardingLocal_CreatesForwarder(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	// First, create the source route and add a publisher so the route is active.
	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("src-pub", PublishModeExclusiveTakeover)
	_, err = srcRoute.AddPublisher(ctx, pub)
	require.NoError(t, err)

	// AddRouteForwardingLocal creates a forwarding from src to dst.
	fwd, err := r.AddRouteForwardingLocal(ctx, "src/stream", "dst/stream", PublishModeExclusiveTakeover, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	// Verify the forwarding was created.
	assert.Equal(t, RoutePath("src/stream"), fwd.SrcPath)
	assert.Equal(t, PublishModeExclusiveTakeover, fwd.PublishMode)

	// Clean up.
	err = fwd.Close(ctx)
	assert.NoError(t, err)
}

func TestRouteForwarding_StartLocked_ContextCancelled(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Create a forwarding struct with a src route that doesn't exist.
	fwd := &RouteForwarding[any]{
		Router:          r,
		SrcPath:         "nonexistent",
		GetSrcRouteMode: GetRouteModeFailIfNotFound,
		CancelFunc:      nil,
	}

	fwd.CancelFunc = func() {}
	err := fwd.start(ctx)
	assert.Error(t, err)
}

func TestRouteForwarding_GetInputNode_NoPublishers(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	inputRoute, err := r.GetRoute(ctx, "input/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{
		Input: inputRoute,
	}

	// GetInputNode with no publishers should return nil.
	result := fwd.GetInputNode(ctx)
	assert.Nil(t, result)
}

func TestRouteForwarding_GetInputNode_WithPublisher(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	inputRoute, err := r.GetRoute(ctx, "input/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	pub := newMockPublisher("pub1", PublishModeExclusiveTakeover)
	_, err = inputRoute.AddPublisher(ctx, pub)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{
		Input: inputRoute,
	}

	// GetInputNode should return nil because the mock publisher's GetInputNode returns nil.
	result := fwd.GetInputNode(ctx)
	assert.Nil(t, result)
}

func TestRouteForwarding_GetOutputRoute_WithOutput(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	outputRoute, err := r.GetRoute(ctx, "output/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	output := &forwardOutputNodeLocalPath[any]{
		NodeRouting: outputRoute.Node,
	}

	fwd := &RouteForwarding[any]{
		Output: output,
	}

	result := fwd.GetOutputRoute(ctx)
	assert.Same(t, outputRoute, result)
}

func TestForwardOutputNodeLocalPath_Close(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	route, err := r.GetRoute(ctx, "test/path", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd := &RouteForwarding[any]{}

	// Add the fwd as a publisher on the route.
	pub := newMockPublisher("fwd-pub", PublishModeExclusiveTakeover)
	_, err = route.AddPublisher(ctx, pub)
	require.NoError(t, err)

	n := &forwardOutputNodeLocalPath[any]{
		NodeRouting:     route.Node,
		RouteForwarding: fwd,
	}

	// Close should try to remove the publisher. Since fwd is not actually the publisher,
	// this should return an error (publisher not found).
	err = n.Close(ctx)
	assert.Error(t, err)
}

func TestRouteForwarding_StopLocked_WithOutput(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	outputRoute, err := r.GetRoute(ctx, "output/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	output := &forwardOutputNodeLocalPath[any]{
		NodeRouting: outputRoute.Node,
	}

	fwd := &RouteForwarding[any]{
		Output: output,
	}

	var wg sync.WaitGroup
	fwd.Locker.Do(ctx, func() {
		err = fwd.stopLocked(ctx, &wg)
	})
	wg.Wait()
	assert.NoError(t, err)
	assert.Nil(t, fwd.Output)
}
