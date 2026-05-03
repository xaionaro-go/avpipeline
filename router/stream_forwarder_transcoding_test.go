package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
)

// fakeOutputPushToCondition is a minimal Condition for use in
// plumbing tests — Match always returns true and the call is recorded.
type fakeOutputPushToCondition struct{}

func (fakeOutputPushToCondition) String() string { return "fakeOutputPushToCondition" }

func (fakeOutputPushToCondition) Match(_ context.Context, _ packetorframefiltercondition.Input) bool {
	return true
}

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
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, nil,
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
		ctx, srcRoute.Node, dstRoute.Node, nil, nil, nil,
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
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	// Should be a StreamForwarderTranscoding.
	_, ok := fwd.(*StreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting])
	assert.True(t, ok, "NewStreamForwarder with transcoderConfig should create StreamForwarderTranscoding")
}

func TestNewStreamForwarder_PassesOutputPushToConditions(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cfg := &transcodertypes.TranscoderConfig{}
	conds := []packetorframefiltercondition.Condition{fakeOutputPushToCondition{}}
	fwd, err := NewStreamForwarder[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, conds,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	tFwd, ok := fwd.(*StreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting])
	require.True(t, ok)
	require.Len(t, tFwd.OutputPushToConditions, 1)
	assert.Equal(t, conds[0], tFwd.OutputPushToConditions[0])
}

func TestNewStreamForwarderTranscoding_StoresOutputPushToConditions(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cfg := &transcodertypes.TranscoderConfig{}
	conds := []packetorframefiltercondition.Condition{fakeOutputPushToCondition{}}
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, conds,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	require.Len(t, fwd.OutputPushToConditions, 1)
	assert.Equal(t, conds[0], fwd.OutputPushToConditions[0])
}

func TestNewStreamForwarderTranscoding_NilOutputPushToConditions(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	cfg := &transcodertypes.TranscoderConfig{}
	fwd, err := NewStreamForwarderTranscoding[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	assert.Empty(t, fwd.OutputPushToConditions)
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
		ctx, srcRoute.Node, dstRoute.Node, cfg, nil, nil,
	)
	require.NoError(t, err)

	// Stop without Start: CancelFunc is nil, should return ErrAlreadyClosed.
	err = fwd.Stop(ctx)
	assert.ErrorIs(t, err, ErrAlreadyClosed{})
}
