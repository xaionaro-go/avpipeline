package nodewrapper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

func newInnerNode(ctx context.Context) node.Abstract {
	return node.NewFromKernel(ctx, &kernel.Passthrough{})
}

func TestNoServe_Serve_IsNoOp(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	errCh := make(chan node.Error, 10)
	// Serve should return immediately (no-op)
	ns.Serve(ctx, node.ServeConfig{}, errCh)

	select {
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	default:
	}
}

func TestNoServe_GetObjectID(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	// NoServe has its own ObjectID, different from the inner node
	nsID := ns.GetObjectID()
	innerID := inner.GetObjectID()
	assert.NotEqual(t, nsID, innerID)
	assert.NotEqual(t, nsID, 0)
}

func TestNoServe_OriginalNode(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner, ns.OriginalNode())
}

func TestNoServe_OriginalNodeAbstract(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	result := ns.OriginalNodeAbstract()
	assert.Equal(t, inner, result)
}

func TestNoServe_OriginalNodeAbstract_NilNode(t *testing.T) {
	ns := &NoServe[node.Abstract]{Node: nil}
	result := ns.OriginalNodeAbstract()
	assert.Nil(t, result)
}

func TestNoServe_String_WithStringer(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	s := ns.String()
	assert.Contains(t, s, "NoServe(")
}

func TestNoServe_String_NilNode(t *testing.T) {
	// When Node is nil, the type assertion for fmt.Stringer fails
	ns := &NoServe[node.Abstract]{Node: nil}
	assert.Equal(t, "NoServe", ns.String())
}

func TestNoServe_GetPushTos_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	result := ns.GetPushTos(ctx)
	assert.Nil(t, result)
}

func TestNoServe_RemovePushTo_ReturnsError(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}
	other := newInnerNode(ctx)

	err := ns.RemovePushTo(ctx, other)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NoServe cannot remove PushTo")
}

func TestNoServe_IsServing_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner.IsServing(ctx), ns.IsServing(ctx))
}

func TestNoServe_GetCountersPtr_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner.GetCountersPtr(), ns.GetCountersPtr())
}

func TestNoServe_GetProcessor_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner.GetProcessor(), ns.GetProcessor())
}

func TestNoServe_GetInputFilter_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner.GetInputFilter(ctx), ns.GetInputFilter(ctx))
}

func TestNoServe_IsDrained_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	assert.Equal(t, inner.IsDrained(ctx), ns.IsDrained(ctx))
}

func TestNoServe_Flush_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	err := ns.Flush(ctx)
	assert.NoError(t, err)
}

func TestNoServe_GetChangeChanIsServing(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	ch := ns.GetChangeChanIsServing()
	assert.NotNil(t, ch)
}

func TestNoServe_GetChangeChanPushTo(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	ch := ns.GetChangeChanPushTo()
	assert.NotNil(t, ch)
}

func TestNoServe_GetChangeChanDrained(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	ch := ns.GetChangeChanDrained()
	assert.NotNil(t, ch)
}

func TestNoServe_ImplementsAbstract(t *testing.T) {
	// Verify NoServe implements node.Abstract at compile time
	var _ node.Abstract = (*NoServe[node.Abstract])(nil)
}

func TestNoServe_WithPushTos_IsNoOp(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	called := false
	ns.WithPushTos(ctx, func(ctx context.Context, pt *node.PushTos) {
		called = true
	})
	assert.False(t, called)
}

func TestNoServe_AddPushTo_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}
	other := newInnerNode(ctx)

	// AddPushTo logs an error but should not panic
	ns.AddPushTo(ctx, other)
}

func TestNoServe_SetPushTos_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	// SetPushTos logs an error but should not panic
	ns.SetPushTos(ctx, nil)
}

func TestNoServe_SetInputFilter_DelegatesToInner(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	// Setting nil filter should work
	ns.SetInputFilter(ctx, nil)
	assert.Nil(t, ns.GetInputFilter(ctx))
}

func TestNoServe_DotBlockContentStringWriteTo(t *testing.T) {
	ctx := context.Background()
	inner := newInnerNode(ctx)
	ns := &NoServe[node.Abstract]{Node: inner}

	buf := &stringBuffer{}
	alreadyPrinted := make(map[processor.Abstract]struct{})
	ns.DotBlockContentStringWriteTo(buf, alreadyPrinted)
}

// stringBuffer is a minimal io.Writer for testing
type stringBuffer struct {
	data []byte
}

func (b *stringBuffer) Write(p []byte) (n int, err error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *stringBuffer) String() string {
	return string(b.data)
}

