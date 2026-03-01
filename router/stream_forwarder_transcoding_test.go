package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
)

func TestNewStreamForwarderTranscoding_NoPacketSource(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// The route node's processor is FromKernel[*NodeKernel], which does
	// implement GetPacketSourcer. But NodeKernel's FormatContext may not have
	// streams, so let's test that the constructor works.
	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	assert.Same(t, srcRoute.Node, fwd.Source())
	assert.NotNil(t, fwd.Destination())
}

func TestNewStreamForwarderTranscoding_NilConfig(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	// With nil config, it should auto-generate a copy config.
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	assert.Same(t, srcRoute.Node, fwd.Source())
	assert.NotNil(t, fwd.Destination())
}

func TestNewStreamForwarder_WithTranscoderConfig(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarder[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	// Should be a StreamForwarderTranscoding.
	_, ok := fwd.(*StreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting])
	assert.True(t, ok, "NewStreamForwarder with transcoderConfig should create StreamForwarderTranscoding")
}

func TestStreamForwarderTranscoding_StopWithoutStart(t *testing.T) {
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

	// Stop without Start: CancelFunc is nil, should return ErrAlreadyClosed.
	err = fwd.Stop(ctx)
	assert.ErrorIs(t, err, ErrAlreadyClosed{})
}
