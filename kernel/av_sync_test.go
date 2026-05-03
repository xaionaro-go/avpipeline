// av_sync_test.go contains tests for the AVSync passthrough kernel.

package kernel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

// avSyncTestHarness owns an Output (only used for its FormatContext to
// register streams against) and a packet source. Each test gets a fresh
// audio + video stream pair with timebase 1/1000 (millisecond precision).
type avSyncTestHarness struct {
	ctx    context.Context
	out    *Output
	src    *dummySource
	audio  *astiav.Stream
	video  *astiav.Stream
	chOut  chan packetorframe.OutputUnion
	cancel context.CancelFunc
}

func newAVSyncTestHarness(t *testing.T) *avSyncTestHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)

	audio := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	audio.SetIndex(0)
	audio.SetTimeBase(astiav.NewRational(1, 1000))
	audio.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	video := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	video.SetIndex(1)
	video.SetTimeBase(astiav.NewRational(1, 1000))
	video.CodecParameters().SetMediaType(astiav.MediaTypeVideo)

	src := &dummySource{FormatContext: out.FormatContext}

	return &avSyncTestHarness{
		ctx:    ctx,
		out:    out,
		src:    src,
		audio:  audio,
		video:  video,
		chOut:  make(chan packetorframe.OutputUnion, 64),
		cancel: cancel,
	}
}

func (h *avSyncTestHarness) close(t *testing.T) {
	t.Helper()
	h.cancel()
	require.NoError(t, h.out.Close(h.ctx))
}

// sendPacket builds a packet input on the given stream with the given
// PTS+DTS in the stream's timebase units, sends it through k, and
// returns the kernel's first emitted output (or nil on timeout).
func (h *avSyncTestHarness) sendPacket(
	t *testing.T,
	k *AVSync,
	stream *astiav.Stream,
	pts, dts int64,
) packetorframe.OutputUnion {
	t.Helper()
	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(stream.Index())
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: h.src})
	require.NoError(t, k.SendInput(h.ctx, packetorframe.InputUnion{Packet: &input}, h.chOut))
	select {
	case out := <-h.chOut:
		return out
	case <-time.After(time.Second):
		t.Fatalf("kernel produced no output for stream=%d pts=%d", stream.Index(), pts)
		return packetorframe.OutputUnion{}
	}
}

// sendPacketNoExpect sends a packet without blocking the caller waiting
// for it to come out — used by the concurrency test where draining
// happens from another goroutine.
func (h *avSyncTestHarness) sendPacketNoExpect(
	t *testing.T,
	k *AVSync,
	stream *astiav.Stream,
	pts, dts int64,
) {
	t.Helper()
	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(stream.Index())
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: h.src})
	require.NoError(t, k.SendInput(h.ctx, packetorframe.InputUnion{Packet: &input}, h.chOut))
}

func ms(d int64) int64 { return d } // alias: 1/1000 timebase => units == milliseconds

// === Pure helper tests ===

func TestAbsDuration(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{0, 0},
		{time.Second, time.Second},
		{-time.Second, time.Second},
		{-time.Nanosecond, time.Nanosecond},
	}
	for _, c := range cases {
		testifyassert.Equal(t, c.want, absDuration(c.in), "in=%v", c.in)
	}
}

func TestSignOf(t *testing.T) {
	testifyassert.Equal(t, 0, signOf(0))
	testifyassert.Equal(t, 1, signOf(time.Nanosecond))
	testifyassert.Equal(t, 1, signOf(time.Hour))
	testifyassert.Equal(t, -1, signOf(-time.Nanosecond))
	testifyassert.Equal(t, -1, signOf(-time.Hour))
}

func TestShouldLogAVSync(t *testing.T) {
	// Below floor delta = 5ms; above floor = 50ms / 200ms.
	below := avSyncSnapshot{audioPTS: 5 * time.Millisecond, videoPTS: 0, hasAudio: true, hasVideo: true}
	above := avSyncSnapshot{audioPTS: 50 * time.Millisecond, videoPTS: 0, hasAudio: true, hasVideo: true}
	aboveBig := avSyncSnapshot{audioPTS: 200 * time.Millisecond, videoPTS: 0, hasAudio: true, hasVideo: true}
	negAbove := avSyncSnapshot{audioPTS: 0, videoPTS: 50 * time.Millisecond, hasAudio: true, hasVideo: true}
	notObserved := avSyncSnapshot{audioPTS: 50 * time.Millisecond, hasAudio: true}

	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name       string
		prev       avSyncLogState
		cur        avSyncSnapshot
		now        time.Time
		wantEmit   bool
		wantReason string
	}{
		{
			name:       "not_observed_no_emit",
			prev:       avSyncLogState{},
			cur:        notObserved,
			now:        now,
			wantEmit:   false,
			wantReason: "",
		},
		{
			name:       "initial",
			prev:       avSyncLogState{},
			cur:        below,
			now:        now,
			wantEmit:   true,
			wantReason: "initial",
		},
		{
			name:       "emerged_below_to_above_floor",
			prev:       avSyncLogState{prev: below, at: now, set: true},
			cur:        above,
			now:        now,
			wantEmit:   true,
			wantReason: "emerged",
		},
		{
			name:       "resolved_above_to_below_floor",
			prev:       avSyncLogState{prev: above, at: now, set: true},
			cur:        below,
			now:        now,
			wantEmit:   true,
			wantReason: "resolved",
		},
		{
			name:       "flip_pos_to_neg",
			prev:       avSyncLogState{prev: above, at: now, set: true},
			cur:        negAbove,
			now:        now,
			wantEmit:   true,
			wantReason: "flip",
		},
		{
			name:       "flip_neg_to_pos",
			prev:       avSyncLogState{prev: negAbove, at: now, set: true},
			cur:        above,
			now:        now,
			wantEmit:   true,
			wantReason: "flip",
		},
		{
			name:       "magnitude_growth_4x",
			prev:       avSyncLogState{prev: above, at: now, set: true}, // 50ms
			cur:        aboveBig,                                        // 200ms
			now:        now,
			wantEmit:   true,
			wantReason: "magnitude",
		},
		{
			name:       "magnitude_shrink_4x",
			prev:       avSyncLogState{prev: aboveBig, at: now, set: true}, // 200ms
			cur:        above,                                              // 50ms
			now:        now,
			wantEmit:   true,
			wantReason: "magnitude",
		},
		{
			name:       "periodic_after_long_stable",
			prev:       avSyncLogState{prev: above, at: now, set: true},
			cur:        above,
			now:        now.Add(avSyncPeriodic + time.Second),
			wantEmit:   true,
			wantReason: "periodic",
		},
		{
			name:       "no_emit_stable_below_period",
			prev:       avSyncLogState{prev: above, at: now, set: true},
			cur:        above,
			now:        now.Add(time.Second),
			wantEmit:   false,
			wantReason: "",
		},
		{
			name:       "no_emit_both_below_floor_stable",
			prev:       avSyncLogState{prev: below, at: now, set: true},
			cur:        below,
			now:        now.Add(time.Second),
			wantEmit:   false,
			wantReason: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			emit, reason := shouldLogAVSync(c.prev, c.cur, c.now)
			testifyassert.Equal(t, c.wantEmit, emit)
			testifyassert.Equal(t, c.wantReason, reason)
		})
	}
}

// === State + offsets ===

func TestAVSync_GetDelta_BeforeAnyInput(t *testing.T) {
	k := NewAVSync(context.Background())
	d, ok := k.GetDelta(context.Background())
	testifyassert.False(t, ok)
	testifyassert.Equal(t, time.Duration(0), d)
}

func TestAVSync_GetDelta_AudioOnly(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	h.sendPacket(t, k, h.audio, ms(1000), ms(1000))

	d, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
	testifyassert.Equal(t, time.Duration(0), d)
}

func TestAVSync_GetDelta_VideoOnly(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	h.sendPacket(t, k, h.video, ms(1000), ms(1000))

	d, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
	testifyassert.Equal(t, time.Duration(0), d)
}

func TestAVSync_GetDelta_BothObserved(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	h.sendPacket(t, k, h.audio, ms(5000), ms(5000)) // 5s
	h.sendPacket(t, k, h.video, ms(1000), ms(1000)) // 1s

	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 4*time.Second, d)
}

func TestAVSync_GetDelta_ZeroPTSObserved(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	h.sendPacket(t, k, h.audio, 0, 0)
	h.sendPacket(t, k, h.video, 0, 0)

	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok, "PTS=0 must still flip hasAudio/hasVideo")
	testifyassert.Equal(t, time.Duration(0), d)
}

func TestAVSync_GetDelta_TakesMaxPerType(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	h.sendPacket(t, k, h.audio, ms(2000), ms(2000)) // 2s
	h.sendPacket(t, k, h.audio, ms(1000), ms(1000)) // 1s — older, should NOT lower the max
	h.sendPacket(t, k, h.video, ms(500), ms(500))   // 0.5s

	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 2*time.Second-500*time.Millisecond, d)
}

func TestAVSync_NoPtsValueSkipped(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Audio packet with no PTS.
	out := h.sendPacket(t, k, h.audio, astiav.NoPtsValue, astiav.NoPtsValue)
	require.NotNil(t, out.Packet, "packet must still be forwarded")

	// Video with valid PTS so the kernel sees video.
	h.sendPacket(t, k, h.video, ms(1000), ms(1000))

	// Audio side must still be unobserved.
	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok, "audio with no-PTS must not flip hasAudio")
}

func TestAVSync_InvalidTimebaseSkipped(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Stream with zero timebase: build a fresh stream and zero its timebase.
	bad := h.out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	bad.SetIndex(2)
	bad.SetTimeBase(astiav.NewRational(0, 0))
	bad.CodecParameters().SetMediaType(astiav.MediaTypeAudio)

	out := h.sendPacket(t, k, bad, 1000, 1000)
	require.NotNil(t, out.Packet, "packet must still be forwarded")

	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok, "invalid timebase must not produce observation")
}

func TestAVSync_NonAudioVideoSkipped(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Subtitle stream — neither audio nor video.
	subEnc := astiav.FindEncoder(astiav.CodecIDWebvtt)
	if subEnc == nil {
		// Fallback: try a different subtitle encoder.
		subEnc = astiav.FindEncoder(astiav.CodecIDSubrip)
	}
	require.NotNil(t, subEnc, "no subtitle encoder available")
	sub := h.out.FormatContext.NewStream(subEnc)
	sub.SetIndex(2)
	sub.SetTimeBase(astiav.NewRational(1, 1000))
	sub.CodecParameters().SetMediaType(astiav.MediaTypeSubtitle)

	out := h.sendPacket(t, k, sub, ms(1000), ms(1000))
	require.NotNil(t, out.Packet, "non-A/V packet must still be forwarded")

	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
}

func TestAVSync_SendInput_Forwards(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	const n = 5
	for i := int64(0); i < n; i++ {
		out := h.sendPacket(t, k, h.audio, ms((i+1)*100), ms((i+1)*100))
		require.NotNil(t, out.Packet)
		testifyassert.Equal(t, ms((i+1)*100), out.GetPTS())
	}
}

func TestAVSync_SetOffset_AppliesToOutgoingPackets(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeVideo, 500*time.Millisecond))

	out := h.sendPacket(t, k, h.video, ms(1000), ms(1000))
	require.NotNil(t, out.Packet)
	// Stream timebase 1/1000 → 500ms = 500 units. Outgoing PTS == 1000+500 = 1500.
	testifyassert.Equal(t, int64(1500), out.GetPTS())
	testifyassert.Equal(t, int64(1500), out.GetDTS())
}

func TestAVSync_SetOffset_AudioAndVideoIndependent(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 100*time.Millisecond))
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeVideo, 800*time.Millisecond))

	// Audio packet: PTS should bump by +100ms.
	outA := h.sendPacket(t, k, h.audio, ms(2000), ms(2000))
	testifyassert.Equal(t, int64(2100), outA.GetPTS())
	testifyassert.Equal(t, int64(2100), outA.GetDTS())

	// Video packet: PTS should bump by +800ms — audio offset must not leak.
	outV := h.sendPacket(t, k, h.video, ms(2000), ms(2000))
	testifyassert.Equal(t, int64(2800), outV.GetPTS())
	testifyassert.Equal(t, int64(2800), outV.GetDTS())
}

func TestAVSync_SetOffset_UnsupportedMediaType(t *testing.T) {
	k := NewAVSync(context.Background())
	err := k.SetOffset(context.Background(), astiav.MediaTypeSubtitle, 100*time.Millisecond)
	require.Error(t, err)
}

func TestAVSync_GetOffset_RoundTrip(t *testing.T) {
	ctx := context.Background()
	k := NewAVSync(ctx)
	require.NoError(t, k.SetOffset(ctx, astiav.MediaTypeAudio, 250*time.Millisecond))
	require.NoError(t, k.SetOffset(ctx, astiav.MediaTypeVideo, -750*time.Millisecond))

	a, err := k.GetOffset(ctx, astiav.MediaTypeAudio)
	require.NoError(t, err)
	testifyassert.Equal(t, 250*time.Millisecond, a)
	v, err := k.GetOffset(ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, -750*time.Millisecond, v)

	_, err = k.GetOffset(ctx, astiav.MediaTypeSubtitle)
	require.Error(t, err)
}

func TestAVSync_AutoTune_NotObserved(t *testing.T) {
	k := NewAVSync(context.Background())
	d, err := k.AutoTune(context.Background())
	require.Error(t, err)
	testifyassert.True(t, errors.Is(err, ErrAVSyncNotObserved), "expected ErrAVSyncNotObserved, got %v", err)
	testifyassert.Equal(t, time.Duration(0), d)
}

func TestAVSync_AutoTune_PositiveDelta_AppliesToVideo(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Audio at 2s, video at 1s — delta = +1s (audio leads video).
	h.sendPacket(t, k, h.audio, ms(2000), ms(2000))
	h.sendPacket(t, k, h.video, ms(1000), ms(1000))

	d, err := k.AutoTune(h.ctx)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Second, d)

	// Subsequent video packet PTS must be shifted forward by +1s.
	out := h.sendPacket(t, k, h.video, ms(3000), ms(3000))
	testifyassert.Equal(t, int64(4000), out.GetPTS(), "video PTS should be shifted +1s")
	testifyassert.Equal(t, int64(4000), out.GetDTS())

	// Audio offset must be untouched.
	a, err := k.GetOffset(h.ctx, astiav.MediaTypeAudio)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Duration(0), a)
}

func TestAVSync_AutoTune_NegativeDelta_Errors(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Audio at 1s, video at 2s — delta = -1s (video leads audio).
	h.sendPacket(t, k, h.audio, ms(1000), ms(1000))
	h.sendPacket(t, k, h.video, ms(2000), ms(2000))

	d, err := k.AutoTune(h.ctx)
	require.Error(t, err)
	testifyassert.True(t, errors.Is(err, ErrAVSyncVideoLeadsAudio), "expected ErrAVSyncVideoLeadsAudio, got %v", err)
	testifyassert.Equal(t, -time.Second, d)

	// Video offset must remain zero.
	v, err := k.GetOffset(h.ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Duration(0), v)
}

func TestAVSync_AutoTune_ZeroDelta_NoOp(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Equal PTS — already in sync.
	h.sendPacket(t, k, h.audio, ms(1000), ms(1000))
	h.sendPacket(t, k, h.video, ms(1000), ms(1000))

	d, err := k.AutoTune(h.ctx)
	require.NoError(t, err, "zero delta is treated as no-op (nil error)")
	testifyassert.Equal(t, time.Duration(0), d)

	v, err := k.GetOffset(h.ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Duration(0), v, "video offset must remain zero")
}

func TestAVSync_AutoTune_DoesNotTouchAudioOffset(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	// Pre-set a non-zero audio offset and confirm AutoTune leaves it alone.
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 250*time.Millisecond))

	// Audio at (2s observed; recall offset is post-applied → raw 2s − offset 0.25s
	// is not how it works: the offset is added to incoming PTS, so observed
	// audioPTS = raw + 0.25s). Use raw audio PTS = 1.75s so observed = 2s.
	h.sendPacket(t, k, h.audio, ms(1750), ms(1750))
	h.sendPacket(t, k, h.video, ms(1000), ms(1000))

	d, err := k.AutoTune(h.ctx)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Second, d)

	a, err := k.GetOffset(h.ctx, astiav.MediaTypeAudio)
	require.NoError(t, err)
	testifyassert.Equal(t, 250*time.Millisecond, a, "AutoTune must not touch the audio offset")

	v, err := k.GetOffset(h.ctx, astiav.MediaTypeVideo)
	require.NoError(t, err)
	testifyassert.Equal(t, time.Second, v)
}

func TestAVSync_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	h := newAVSyncTestHarness(t)
	defer h.close(t)

	// Use a debug-level logger so observeAndApply's debug branch executes
	// under the race detector.
	ctx := h.ctx
	if logger.FromCtx(ctx).Level() < logger.LevelDebug {
		// Pick a debug-capable logger from the package's existing pattern.
		ctx = logger.CtxWithLogger(ctx, logger.FromCtx(ctx).WithLevel(logger.LevelDebug))
	}

	k := NewAVSync(ctx)

	const writers = 4
	const readers = 4
	const iters = 100

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	stop := make(chan struct{})
	for i := 0; i < writers; i++ {
		stream := h.audio
		if i%2 == 0 {
			stream = h.video
		}
		go func(streamLocal *astiav.Stream, base int64) {
			defer wg.Done()
			for j := int64(0); j < iters; j++ {
				select {
				case <-stop:
					return
				default:
				}
				h.sendPacketNoExpect(t, k, streamLocal, base+j, base+j)
				// Drain.
				select {
				case <-h.chOut:
				case <-time.After(time.Second):
					t.Errorf("drain timeout")
					return
				}
			}
		}(stream, int64(i*1000))
	}

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_, _ = k.GetDelta(ctx)
				_, _ = k.GetOffset(ctx, astiav.MediaTypeAudio)
				_, _ = k.GetOffset(ctx, astiav.MediaTypeVideo)
				_ = k.SetOffset(ctx, astiav.MediaTypeVideo, time.Duration(j)*time.Microsecond)
			}
		}()
	}

	wg.Wait()
	close(stop)
}

// === ApplyAndObserve (used by avsynccondition.Condition) ===

func TestAVSync_ApplyAndObserve_AppliesAudioOffset(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 250*time.Millisecond))

	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(h.audio.Index())
	pkt.SetPts(1000)
	pkt.SetDts(1000)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: h.audio, Source: h.src})
	u := packetorframe.InputUnion{Packet: &input}
	k.ApplyAndObserve(h.ctx, &u)

	// 1/1000 timebase: 250ms = 250 units. Mutated in place.
	testifyassert.Equal(t, int64(1250), u.GetPTS())
	testifyassert.Equal(t, int64(1250), u.GetDTS())
}

func TestAVSync_ApplyAndObserve_AppliesVideoOffset(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeVideo, 750*time.Millisecond))

	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(h.video.Index())
	pkt.SetPts(2000)
	pkt.SetDts(2000)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: h.video, Source: h.src})
	u := packetorframe.InputUnion{Packet: &input}
	k.ApplyAndObserve(h.ctx, &u)

	testifyassert.Equal(t, int64(2750), u.GetPTS())
	testifyassert.Equal(t, int64(2750), u.GetDTS())
}

func TestAVSync_ApplyAndObserve_AudioObservation(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	pktA := packet.Pool.Get()
	pktA.SetStreamIndex(h.audio.Index())
	pktA.SetPts(2500)
	pktA.SetDts(2500)
	inputA := packet.BuildInput(pktA, &packet.StreamInfo{Stream: h.audio, Source: h.src})
	uA := packetorframe.InputUnion{Packet: &inputA}
	k.ApplyAndObserve(h.ctx, &uA)

	pktV := packet.Pool.Get()
	pktV.SetStreamIndex(h.video.Index())
	pktV.SetPts(1000)
	pktV.SetDts(1000)
	inputV := packet.BuildInput(pktV, &packet.StreamInfo{Stream: h.video, Source: h.src})
	uV := packetorframe.InputUnion{Packet: &inputV}
	k.ApplyAndObserve(h.ctx, &uV)

	d, ok := k.GetDelta(h.ctx)
	require.True(t, ok)
	testifyassert.Equal(t, 1500*time.Millisecond, d)
}

func TestAVSync_ApplyAndObserve_PassesUnknownMediaThrough(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)
	require.NoError(t, k.SetOffset(h.ctx, astiav.MediaTypeAudio, 250*time.Millisecond))

	subEnc := astiav.FindEncoder(astiav.CodecIDWebvtt)
	if subEnc == nil {
		subEnc = astiav.FindEncoder(astiav.CodecIDSubrip)
	}
	require.NotNil(t, subEnc, "no subtitle encoder available")
	sub := h.out.FormatContext.NewStream(subEnc)
	sub.SetIndex(2)
	sub.SetTimeBase(astiav.NewRational(1, 1000))
	sub.CodecParameters().SetMediaType(astiav.MediaTypeSubtitle)

	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(sub.Index())
	pkt.SetPts(1000)
	pkt.SetDts(1000)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: sub, Source: h.src})
	u := packetorframe.InputUnion{Packet: &input}
	k.ApplyAndObserve(h.ctx, &u)

	// PTS/DTS untouched.
	testifyassert.Equal(t, int64(1000), u.GetPTS())
	testifyassert.Equal(t, int64(1000), u.GetDTS())
	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
}

func TestAVSync_ApplyAndObserve_NoPtsValueSkipped(t *testing.T) {
	h := newAVSyncTestHarness(t)
	defer h.close(t)
	k := NewAVSync(h.ctx)

	pkt := packet.Pool.Get()
	pkt.SetStreamIndex(h.audio.Index())
	pkt.SetPts(astiav.NoPtsValue)
	pkt.SetDts(astiav.NoPtsValue)
	input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: h.audio, Source: h.src})
	u := packetorframe.InputUnion{Packet: &input}
	k.ApplyAndObserve(h.ctx, &u)

	// hasAudio must remain false.
	pktV := packet.Pool.Get()
	pktV.SetStreamIndex(h.video.Index())
	pktV.SetPts(1000)
	pktV.SetDts(1000)
	inputV := packet.BuildInput(pktV, &packet.StreamInfo{Stream: h.video, Source: h.src})
	uV := packetorframe.InputUnion{Packet: &inputV}
	k.ApplyAndObserve(h.ctx, &uV)

	_, ok := k.GetDelta(h.ctx)
	testifyassert.False(t, ok)
}
