// avsynccondition_test.go contains tests for the AVSync Condition.

package avsynccondition

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

// === Condition satisfies packetorframefiltercondition.Condition ===

func TestCondition_SatisfiesConditionInterface(t *testing.T) {
	var _ packetorframefiltercondition.Condition = (*Condition)(nil)
}

// === Condition.String delegates to AVSync.String ===

func TestCondition_String_DelegatesToAVSync(t *testing.T) {
	ctx := context.Background()
	c := New(kernel.NewAVSync(ctx))

	// kernel.AVSync.String() returns "AVSync" — Condition.String must
	// match exactly (no double-naming, no extra wrapping).
	testifyassert.Equal(t, "AVSync", c.String())
}

// === Condition.Match always returns true ===

func TestCondition_Match_AlwaysReturnsTrue(t *testing.T) {
	ctx := context.Background()
	c := New(kernel.NewAVSync(ctx))

	// Empty input (no packet/frame) — must still return true.
	testifyassert.True(t, c.Match(ctx, packetorframefiltercondition.Input{}))
}

// === Match observation paths ===

type matchHarness struct {
	ctx   context.Context
	out   *kernel.Output
	audio *astiav.Stream
	video *astiav.Stream
}

func newMatchHarness(t *testing.T) *matchHarness {
	t.Helper()
	ctx := context.Background()
	out, err := kernel.NewOutputFromURL(ctx, "", secret.New(""), kernel.OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close(ctx) })

	audio := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	audio.SetIndex(0)
	audio.SetTimeBase(astiav.NewRational(1, 1000))
	audio.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	video := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	video.SetIndex(1)
	video.SetTimeBase(astiav.NewRational(1, 1000))
	video.CodecParameters().SetMediaType(astiav.MediaTypeVideo)

	return &matchHarness{
		ctx:   ctx,
		out:   out,
		audio: audio,
		video: video,
	}
}

func (h *matchHarness) buildInput(stream *astiav.Stream, pts, dts int64) packetorframefiltercondition.Input {
	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(stream.Index())
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream})
	return packetorframefiltercondition.Input{
		Destination: nil,
		Input:       packetorframe.InputUnion{Packet: &input},
	}
}

func TestCondition_Match_AppliesAudioOffset(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 250*time.Millisecond))
	c := New(k)

	in := h.buildInput(h.audio, 1000, 1000)
	require.True(t, c.Match(h.ctx, in))

	// 1/1000 timebase: 250ms = 250 units.
	testifyassert.Equal(t, int64(1250), in.Input.GetPTS())
	testifyassert.Equal(t, int64(1250), in.Input.GetDTS())
}

func TestCondition_Match_AppliesVideoOffset(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeVideo, 750*time.Millisecond))
	c := New(k)

	in := h.buildInput(h.video, 2000, 2000)
	require.True(t, c.Match(h.ctx, in))

	testifyassert.Equal(t, int64(2750), in.Input.GetPTS())
	testifyassert.Equal(t, int64(2750), in.Input.GetDTS())
}

func TestCondition_Match_ObservationCommitted(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	c := New(k)

	require.True(t, c.Match(h.ctx, h.buildInput(h.audio, 3000, 3000)))
	require.True(t, c.Match(h.ctx, h.buildInput(h.video, 1000, 1000)))

	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 2*time.Second, d)
}

func TestCondition_Match_PassesUnknownMediaThrough(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 250*time.Millisecond))
	c := New(k)

	subEnc := astiav.FindEncoder(astiav.CodecIDWebvtt)
	if subEnc == nil {
		subEnc = astiav.FindEncoder(astiav.CodecIDSubrip)
	}
	require.NotNil(t, subEnc, "no subtitle encoder available")
	sub := h.out.FormatContext.NewStream(subEnc)
	sub.SetIndex(2)
	sub.SetTimeBase(astiav.NewRational(1, 1000))
	sub.CodecParameters().SetMediaType(astiav.MediaTypeSubtitle)

	in := h.buildInput(sub, 1000, 1000)
	require.True(t, c.Match(h.ctx, in))

	// PTS/DTS untouched — non-A/V stream is not adjusted.
	testifyassert.Equal(t, int64(1000), in.Input.GetPTS())
	testifyassert.Equal(t, int64(1000), in.Input.GetDTS())

	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
}

func TestCondition_Match_NoPtsValueSkipped(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	c := New(k)

	require.True(t, c.Match(h.ctx, h.buildInput(h.audio, astiav.NoPtsValue, astiav.NoPtsValue)))
	require.True(t, c.Match(h.ctx, h.buildInput(h.video, 1000, 1000)))

	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok, "audio with no-PTS must not flip hasAudio")
}

// === Reset delegation ===

func TestCondition_Reset_SatisfiesResetterInterface(t *testing.T) {
	var _ kerneltypes.Resetter = (*Condition)(nil)
}

func TestCondition_Reset_DelegatesToAVSync(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	c := New(k)

	// Observe both audio and video, then verify the wrapped AVSync
	// reports a delta.
	require.True(t, c.Match(h.ctx, h.buildInput(h.audio, 3000, 3000)))
	require.True(t, c.Match(h.ctx, h.buildInput(h.video, 1000, 1000)))
	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 2*time.Second, d)

	// Calling Reset on the Condition must clear the wrapped AVSync's
	// observation (delegation, not duplication).
	require.NoError(t, c.Reset(h.ctx))
	_, ok = k.GetDelta(h.ctx)
	testifyassert.False(t, ok, "Condition.Reset must clear wrapped AVSync's observation")
}

func TestCondition_Reset_PreservesOffsets(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeVideo, 4*time.Second))
	c := New(k)

	require.NoError(t, c.Reset(h.ctx))

	v, err := k.GetOffset(h.ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, 4*time.Second, v)
}

func TestCondition_Match_FrameNoOp(t *testing.T) {
	h := newMatchHarness(t)
	k := kernel.NewAVSync(h.ctx)
	c := New(k)

	// Frame with empty union shape (no Packet) — must return true and
	// not flip observation. Frames are not observed by AVSync.
	in := packetorframefiltercondition.Input{
		Input: packetorframe.InputUnion{},
	}
	require.True(t, c.Match(h.ctx, in))
	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
}
