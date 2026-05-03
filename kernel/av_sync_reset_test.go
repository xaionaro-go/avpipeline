// av_sync_reset_test.go contains tests for AVSync.Reset, which clears
// per-stream observation state without touching operator-configured
// offsets.

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
)

// === Compile-time interface assertions ===

func TestAVSync_Reset_ResetterInterface(t *testing.T) {
	// Compile-time assertion lives in av_sync.go; this function just
	// ensures the test file compiles against the interface symbol so a
	// future rename or accidental method-removal trips the test pkg too.
	var _ kerneltypes.Resetter = (*AVSync)(nil)
	var _ kerneltypes.Resetter = (*AudioSync)(nil)
}

// === Reset behaviour ===

func TestAVSync_Reset_ClearsObservation(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Pre-reset observation: audio at 200ms, video at 180ms → delta = 20ms.
	h.sendPacket(t, k, h.audio, ms(200), ms(200))
	h.sendPacket(t, k, h.video, ms(180), ms(180))
	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 20*time.Millisecond, d)

	// Reset clears observation.
	require.NoError(t, k.Reset(h.ctx))
	d, ok = k.GetDelta(h.ctx)
	testifyassert.False(t, ok, "after Reset, neither audio nor video should be observed")
	testifyassert.Equal(t, time.Duration(0), d)

	// Post-reset: feed PTS values LOWER than pre-reset values. Without
	// the Reset, the max-PTS retention would keep audioPTS=200ms and
	// drop the new 50ms observation. With the Reset in effect, the new
	// values become the authoritative observation.
	h.sendPacket(t, k, h.audio, ms(50), ms(50))
	h.sendPacket(t, k, h.video, ms(40), ms(40))
	d, ok = k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 10*time.Millisecond, d)
}

func TestAVSync_Reset_PreservesOffsets(t *testing.T) {
	ctx := context.Background()
	k := NewAVSync(ctx)

	// Operator-configured state must survive Reset.
	require.NoError(t, k.SetOffset(ctx, astiav.MediaTypeVideo, 4*time.Second))
	require.NoError(t, k.SetOffset(ctx, astiav.MediaTypeAudio, 250*time.Millisecond))

	require.NoError(t, k.Reset(ctx))

	v, err := k.GetOffset(ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, 4*time.Second, v, "video offset must be preserved across Reset")

	a, err := k.GetOffset(ctx, astiav.MediaTypeAudio)
	require.NoError(t, err)
	testifyassert.Equal(t, 250*time.Millisecond, a, "audio offset must be preserved across Reset")
}

func TestAVSync_Reset_BeforeAnyObservation(t *testing.T) {
	// Reset on a fresh kernel must be a clean no-op (no panic, no error).
	ctx := context.Background()
	k := NewAVSync(ctx)
	require.NoError(t, k.Reset(ctx))
	_, ok := k.GetDelta(ctx)
	testifyassert.False(t, ok)
}
