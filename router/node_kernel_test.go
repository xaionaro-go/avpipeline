package router

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestNewNodeKernel(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)
	require.NotNil(t, k)
	assert.NotNil(t, k.ClosureSignaler)
	assert.NotNil(t, k.FormatContext)
	assert.NotNil(t, k.PreviousSource)
	assert.NotNil(t, k.SourceInfo)
	assert.NotNil(t, k.OutputStreams)
}

func TestNewNodeKernel_WithShouldFixPTS(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)
	require.NotNil(t, k)
	assert.True(t, k.Config.ShouldFixPTS)
}

func TestNewNodeKernel_WithoutShouldFixPTS(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(false))
	require.NoError(t, err)
	require.NotNil(t, k)
	assert.False(t, k.Config.ShouldFixPTS)
}

func TestNewNodeKernel_DefaultConfig(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)
	assert.False(t, k.Config.ShouldFixPTS)
}

func TestNodeKernel_String(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "RoutingNode", k.String())
}

func TestNodeKernel_Close(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	err = k.Close(ctx)
	assert.NoError(t, err)

	// CloseChan should be closed after Close.
	select {
	case <-k.CloseChan():
		// expected
	default:
		t.Fatal("expected CloseChan to be closed after Close()")
	}
}

func TestNodeKernel_CloseChan_NotClosedInitially(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	select {
	case <-k.CloseChan():
		t.Fatal("CloseChan should not be closed initially")
	default:
		// expected
	}
}

func TestNodeKernel_Generate_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err = k.Generate(ctx, outputCh)
	assert.NoError(t, err)
	assert.Empty(t, outputCh, "Generate should not produce any output")
}

func TestNodeKernel_GetObjectID(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	id := k.GetObjectID()
	assert.NotZero(t, id, "ObjectID should be non-zero")
}

func TestNodeKernel_GetObjectID_Unique(t *testing.T) {
	ctx := context.Background()
	k1, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	k2, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	assert.NotEqual(t, k1.GetObjectID(), k2.GetObjectID(), "two different kernels should have different ObjectIDs")
}

func TestNodeKernel_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	var called bool
	k.WithOutputFormatContext(ctx, func(fc *astiav.FormatContext) {
		called = true
		assert.NotNil(t, fc)
	})
	assert.True(t, called)
}

func TestNodeKernel_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	var called bool
	k.WithInputFormatContext(ctx, func(fc *astiav.FormatContext) {
		called = true
		assert.NotNil(t, fc)
	})
	assert.True(t, called)
}

func TestNodeKernelOptions_Config_Empty(t *testing.T) {
	opts := NodeKernelOptions{}
	cfg := opts.config()
	assert.False(t, cfg.ShouldFixPTS)
}

func TestNodeKernelOptions_Config_WithOptions(t *testing.T) {
	opts := NodeKernelOptions{NodeKernelOptionShouldFixPTS(true)}
	cfg := opts.config()
	assert.True(t, cfg.ShouldFixPTS)
}

func TestNodeKernelOptionShouldFixPTS_Apply(t *testing.T) {
	cfg := nodeKernelConfig{}
	opt := NodeKernelOptionShouldFixPTS(true)
	opt.apply(&cfg)
	assert.True(t, cfg.ShouldFixPTS)

	opt2 := NodeKernelOptionShouldFixPTS(false)
	opt2.apply(&cfg)
	assert.False(t, cfg.ShouldFixPTS)
}
