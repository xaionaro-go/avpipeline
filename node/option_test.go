package node

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packet"
)

// mockCacheHandler implements CachingHandler for testing.
type mockCacheHandler struct {
	addPushToCalls    []PushTo
	removePushToCalls []PushTo
	resetCalls        int
}

func (m *mockCacheHandler) OnAddPushTo(ctx context.Context, pushTo PushTo) {
	m.addPushToCalls = append(m.addPushToCalls, pushTo)
}

func (m *mockCacheHandler) OnRemovePushTo(ctx context.Context, pushTo PushTo) {
	m.removePushToCalls = append(m.removePushToCalls, pushTo)
}

func (m *mockCacheHandler) RememberPacketIfNeeded(ctx context.Context, pkt packet.Input) error {
	return nil
}

func (m *mockCacheHandler) GetPending(ctx context.Context, pushTo PushTo) ([]packet.Input, error) {
	return nil, nil
}

func (m *mockCacheHandler) Reset(ctx context.Context) {
	m.resetCalls++
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	tassert.Nil(t, cfg.CacheHandler, "default config should have nil CacheHandler")
}

func TestOptionCacheHandler(t *testing.T) {
	handler := &mockCacheHandler{}
	opt := OptionCacheHandler(handler)

	cfg := defaultConfig()
	opt.apply(&cfg)

	tassert.Equal(t, handler, cfg.CacheHandler)
}

func TestOptions_Config(t *testing.T) {
	handler := &mockCacheHandler{}
	opts := Options{OptionCacheHandler(handler)}
	cfg := opts.config()
	tassert.Equal(t, handler, cfg.CacheHandler)
}

func TestOptions_Config_Empty(t *testing.T) {
	opts := Options{}
	cfg := opts.config()
	tassert.Nil(t, cfg.CacheHandler)
}

func TestNodeWithCacheHandler_AddPushTo(t *testing.T) {
	ctx := context.Background()
	handler := &mockCacheHandler{}
	proc := newDummyNode().Processor

	n := NewWithCustomData[struct{}](proc, OptionCacheHandler(handler))
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)

	tassert.Len(t, handler.addPushToCalls, 1)
	tassert.Equal(t, Abstract(dst), handler.addPushToCalls[0].Node)
}

func TestNodeWithCacheHandler_RemovePushTo(t *testing.T) {
	ctx := context.Background()
	handler := &mockCacheHandler{}
	proc := newDummyNode().Processor

	n := NewWithCustomData[struct{}](proc, OptionCacheHandler(handler))
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)
	err := n.RemovePushTo(ctx, dst)
	tassert.NoError(t, err)

	tassert.Len(t, handler.removePushToCalls, 1)
	tassert.Equal(t, Abstract(dst), handler.removePushToCalls[0].Node)
}

func TestNodeWithCacheHandler_SetPushTos(t *testing.T) {
	ctx := context.Background()
	handler := &mockCacheHandler{}
	proc := newDummyNode().Processor

	n := NewWithCustomData[struct{}](proc, OptionCacheHandler(handler))
	dst := newDummyNode()

	n.SetPushTos(ctx, PushTos{{Node: dst}})

	// SetPushTos should call OnAddPushTo for added entries
	tassert.Len(t, handler.addPushToCalls, 1)
}

func TestNodeWithCacheHandler_WithPushTos_AddRemove(t *testing.T) {
	ctx := context.Background()
	handler := &mockCacheHandler{}
	proc := newDummyNode().Processor

	n := NewWithCustomData[struct{}](proc, OptionCacheHandler(handler))
	dst1 := newDummyNode()
	dst2 := newDummyNode()

	// First add dst1
	n.AddPushTo(ctx, dst1)
	tassert.Len(t, handler.addPushToCalls, 1)

	// Use WithPushTos to replace dst1 with dst2
	n.WithPushTos(ctx, func(ctx context.Context, pts *PushTos) {
		*pts = PushTos{{Node: dst2}}
	})

	// Should have called OnAddPushTo for dst2 and OnRemovePushTo for dst1
	tassert.Len(t, handler.addPushToCalls, 2) // dst1 + dst2
	tassert.Len(t, handler.removePushToCalls, 1)
}
