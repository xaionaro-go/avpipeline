// filter_kernel_factory_test.go tests the FilterKernelFactory threading
// through the forwarding infrastructure.
// auto-generated test

package router

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
)

func TestNewStreamForwarderTranscoding_WithFilterKernelFactory(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	factoryCalled := false
	factory := FilterKernelFactory(func(ctx context.Context) (kernel.Abstract, error) {
		factoryCalled = true
		return nil, nil
	})

	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, factory,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	// Factory is stored but not yet called (called on Start).
	assert.NotNil(t, fwd.FilterKernelFactory)
	assert.False(t, factoryCalled, "factory should not be called during construction")
}

func TestNewStreamForwarderTranscoding_NilFilterKernelFactory(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)
	assert.Nil(t, fwd.FilterKernelFactory)
}

func TestNewStreamForwarder_TranscodingPassesFactory(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	factory := FilterKernelFactory(func(ctx context.Context) (kernel.Abstract, error) {
		return nil, nil
	})

	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarder[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, factory,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	sfTranscoding, ok := fwd.(*StreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting])
	require.True(t, ok, "should be StreamForwarderTranscoding")
	assert.NotNil(t, sfTranscoding.FilterKernelFactory)
}

func TestNewStreamForwarder_CopyIgnoresFactory(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	factory := FilterKernelFactory(func(ctx context.Context) (kernel.Abstract, error) {
		return nil, fmt.Errorf("should never be called for copy")
	})

	// nil transcoderConfig → copy mode, factory should be ignored
	fwd, err := NewStreamForwarder[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, nil, factory,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	_, ok := fwd.(*StreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting])
	assert.True(t, ok, "should be StreamForwarderCopy")
}

func TestRouteForwarding_FilterKernelFactory_Stored(t *testing.T) {
	factory := FilterKernelFactory(func(ctx context.Context) (kernel.Abstract, error) {
		return nil, nil
	})

	fwd := &RouteForwarding[any]{
		FilterKernelFactory: factory,
	}
	assert.NotNil(t, fwd.FilterKernelFactory)
}

func TestRouteSource_FilterKernelFactory_Stored(t *testing.T) {
	factory := FilterKernelFactory(func(ctx context.Context) (kernel.Abstract, error) {
		return nil, nil
	})

	fwd := &RouteSource[any, GoBug63285RouteInterface[any], *ProcessorRouting]{
		FilterKernelFactory: factory,
	}
	assert.NotNil(t, fwd.FilterKernelFactory)
}
