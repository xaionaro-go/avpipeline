package router

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

func TestStreamForwarderCopy_NewStreamForwarderCopy(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	assert.Same(t, srcRoute.Node, fwd.Source())
}

func TestStreamForwarderCopy_String(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	s := fwd.String()
	assert.Contains(t, s, "fwd(")
}

func TestStreamForwarderCopy_Source(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	assert.Same(t, srcRoute.Node, fwd.Source())
}

func TestStreamForwarderCopy_Destination(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	dest := fwd.Destination()
	assert.NotNil(t, dest)
}

func TestStreamForwarderCopy_OutputAsNode_Serve(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()

	// Serve should be a no-op.
	errCh := make(chan node.Error, 1)
	outputNode.Serve(ctx, node.ServeConfig{}, errCh)
	// No error expected, no panic.
}

func TestStreamForwarderCopy_OutputAsNode_GetObjectID(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	id := outputNode.GetObjectID()
	assert.NotZero(t, id)
}

func TestStreamForwarderCopy_OutputAsNode_String(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	s := outputNode.String()
	assert.Contains(t, s, "FwdCpyOutput")
}

func TestStreamForwarderCopy_OutputAsNode_OriginalNodeAbstract(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	orig := outputNode.OriginalNodeAbstract()
	assert.NotNil(t, orig)
	assert.Same(t, fwd.Output, orig)
}

func TestStreamForwarderCopy_OutputAsNode_GetPushTos(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	pushTos := outputNode.GetPushTos(ctx)
	// Should return empty/nil push tos initially.
	assert.Empty(t, pushTos)
}

func TestStreamForwarderCopy_OutputAsNode_WithPushTos(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// WithPushTos delegates to the wrapped output node. Just verify no panic.
	outputNode.WithPushTos(ctx, func(ctx context.Context, pts *node.PushTos) {
		// callback may or may not be called depending on the wrapper implementation
	})
}

func TestStreamForwarderCopy_OutputAsNode_IsServing(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// IsServing() delegates to the underlying node (dstRoute.Node),
	// which IS serving. Just verify it doesn't panic.
	_ = outputNode.IsServing(ctx)
}

func TestStreamForwarderCopy_OutputAsNode_GetProcessor(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	proc := outputNode.GetProcessor()
	assert.NotNil(t, proc)
}

func TestStreamForwarderCopy_OutputAsNode_GetInputFilter(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	filter := outputNode.GetInputFilter(ctx)
	// May be nil initially.
	_ = filter
}

func TestStreamForwarderCopy_OutputAsNode_SetInputFilter(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// SetInputFilter with nil should not panic.
	outputNode.SetInputFilter(ctx, nil)
}

func TestStreamForwarderCopy_OutputAsNode_GetChangeChanIsServing(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	ch := outputNode.GetChangeChanIsServing()
	assert.NotNil(t, ch)
}

func TestStreamForwarderCopy_OutputAsNode_GetChangeChanPushTo(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	ch := outputNode.GetChangeChanPushTo()
	assert.NotNil(t, ch)
}

func TestStreamForwarderCopy_OutputAsNode_GetChangeChanDrained(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	ch := outputNode.GetChangeChanDrained()
	assert.NotNil(t, ch)
}

func TestStreamForwarderCopy_OutputAsNode_IsDrained(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// Should not panic.
	_ = outputNode.IsDrained(ctx)
}

func TestStreamForwarderCopy_OutputAsNode_Flush(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	err = outputNode.Flush(ctx)
	assert.NoError(t, err)
}

func TestStreamForwarderCopy_OutputAsNode_GetCountersPtr(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	counters := outputNode.GetCountersPtr()
	assert.NotNil(t, counters)
}

func TestStreamForwarderCopy_OutputAsNode_DotBlockContentStringWriteTo(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// Pass a non-nil writer to avoid nil pointer dereference in the wrapped node.
	var buf bytes.Buffer
	alreadyPrinted := make(map[processor.Abstract]struct{})
	outputNode.DotBlockContentStringWriteTo(&buf, alreadyPrinted)
}

func TestStreamForwarderCopy_OutputAsNode_AddPushTo(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	extraRoute, err := r.GetRoute(ctx, "extra/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// AddPushTo delegates to the wrapped output. The NoServe wrapper logs
	// an error but doesn't support PushTo, so we just verify no panic.
	outputNode.AddPushTo(ctx, extraRoute.Node)
}

func TestStreamForwarderCopy_OutputAsNode_SetPushTos(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// SetPushTos delegates to the wrapped output. Just verify no panic.
	outputNode.SetPushTos(ctx, nil)
}

func TestStreamForwarderCopy_OutputAsNode_RemovePushTo(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	extraRoute, err := r.GetRoute(ctx, "extra/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	outputNode := fwd.outputAsNode()
	// RemovePushTo may return error since nothing was added. Just verify no panic.
	_ = outputNode.RemovePushTo(ctx, extraRoute.Node)
}

func TestStreamForwarderCopy_StartAndStop(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node,
	)
	require.NoError(t, err)

	err = fwd.Start(ctx)
	require.NoError(t, err)

	// Start again should fail (already open).
	err = fwd.Start(ctx)
	assert.ErrorIs(t, err, ErrAlreadyOpen{})

	// Stop.
	err = fwd.Stop(ctx)
	assert.NoError(t, err)

	// Stop again should fail (already closed).
	err = fwd.Stop(ctx)
	assert.ErrorIs(t, err, ErrAlreadyClosed{})
}

func TestNewStreamForwarder_NilTranscoderConfig_CreatesCopy(t *testing.T) {
	r := newTestRouter(t)
	ctx := context.Background()

	srcRoute, err := r.GetRoute(ctx, "src/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)
	dstRoute, err := r.GetRoute(ctx, "dst/stream", GetRouteModeCreateTemporary)
	require.NoError(t, err)

	fwd, err := NewStreamForwarder[GoBug63285RouteInterface[any], *ProcessorRouting](
		ctx, srcRoute.Node, dstRoute.Node, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fwd)

	// Should be a StreamForwarderCopy.
	_, ok := fwd.(*StreamForwarderCopy[GoBug63285RouteInterface[any], *ProcessorRouting])
	assert.True(t, ok, "NewStreamForwarder with nil transcoderConfig should create StreamForwarderCopy")
}
