// Tests for audio-epoch alignment in the multi-source / mid-stream-arrival
// case that the Bug 5b refactor must satisfy.
//
// In production, ffstream's split-AV mux pushes audio (aac-48000 cascade)
// and video (regexp <codec>-<height> cascade) as INDEPENDENT publishers
// that the router's NodeKernel sees as two distinct packet sources for
// the same composite stream. Either side can attach first; once both are
// present, a consumer attaching to the merged route must see audio and
// video on a coherent shared epoch so that downstream cross-stream DTS
// reorder + Output's WaitForOutputStreams gate can release the FLV
// header.
//
// The shipped commit 376cf84 ("router: realign audio epoch on new packet
// source") only realigns audio when the FIRST audio packet from a new
// audio source arrives AFTER LatestPTS has already advanced. When the
// audio publisher attaches BEFORE the video publisher (the actual prod
// arrival order — audio side opens first because the audio cascade is
// shorter), the first audio packet sees LatestPTS==0, the realignment
// gate silently no-ops, audioEpochComputed stays false, and every
// subsequent audio packet from the same source has setNewTimeShift=false
// so the gate never re-fires once video finally arrives — audio remains
// pinned to the publisher's wall-clock epoch (e.g. 5s ahead of the
// video-relative zero) for the entire session.
//
// These tests pin down the desired post-fix behavior:
//
//   1. AudioFirstThenVideo_Aligns: a fresh kernel that sees audio packets
//      before any video must defer the epoch computation, then perform it
//      on the first audio packet that arrives AFTER LatestPTS has been
//      established by the video side. Audio output PTS must land near
//      LatestPTS, not multi-seconds ahead.
//
//   2. PerSourceTracking_FreshSecondAudioSource: when a SECOND audio
//      source (different *packet.Source instance) attaches mid-stream,
//      the kernel must compute its own epoch — independent of any prior
//      source's offset — so a publisher reconnect cannot smuggle the
//      previous session's offset into the new session.
//
//   3. FreshSinglePublisher_NoRegression: a brand-new kernel with one
//      publisher whose audio leads video by tiny amounts (<2s typical
//      arrival jitter) must NOT compute a spurious epoch — the audio
//      should pass through unmodified (or with rate-correction only).
//      This is the regression the prior subagent's sketch hit: applying
//      the broader gate eagerly would shift fresh-publisher audio when
//      no shift is needed.

package router

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// audioPacket builds a single audio Input with the given DTS/PTS in the
// 1/1000 timebase typical of FLV/RTMP feeds, attached to the supplied
// packet.Source so the kernel's per-source bookkeeping can distinguish
// publishers.
func audioPacket(t *testing.T, src packet.Source, streamIndex int, dts, pts int64) packet.Input {
	t.Helper()
	tb := astiav.NewRational(1, 1000)
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(streamIndex)
	pkt.SetDts(dts)
	pkt.SetPts(pts)

	stream := astiav.AllocFormatContext().NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream.CodecParameters().SetSampleRate(48000)
	stream.SetTimeBase(tb)
	stream.SetIndex(streamIndex)

	info := &packet.StreamInfo{
		Stream:   stream,
		Source:   src,
		TimeBase: tb,
	}
	return packet.BuildInput(pkt, info)
}

// videoPacket builds a single video Input in the 1/1000 timebase. The
// kernel's video branch updates LatestPTS via its deferred PTS commit;
// these helpers keep the test focused on epoch alignment, not on rate
// or codec details.
func videoPacket(t *testing.T, src packet.Source, streamIndex int, dts, pts int64) packet.Input {
	t.Helper()
	tb := astiav.NewRational(1, 1000)
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(streamIndex)
	pkt.SetDts(dts)
	pkt.SetPts(pts)

	stream := astiav.AllocFormatContext().NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(tb)
	stream.SetIndex(streamIndex)

	info := &packet.StreamInfo{
		Stream:   stream,
		Source:   src,
		TimeBase: tb,
	}
	return packet.BuildInput(pkt, info)
}

func sendPacketAndDrain(
	t *testing.T,
	ctx context.Context,
	k *NodeKernel,
	in packet.Input,
	outputCh chan packetorframe.OutputUnion,
) (out *packet.Output) {
	t.Helper()
	err := k.SendInput(ctx, packetorframe.InputUnion{Packet: &in}, outputCh)
	require.NoError(t, err)
	select {
	case got := <-outputCh:
		return got.Packet
	case <-time.After(2 * time.Second):
		t.Fatalf("expected packet on outputCh, got none")
		return nil
	}
}

// TestNodeKernel_AudioFirstThenVideo_Aligns is the core RED test for the
// multi-cascade prod scenario: audio publisher A attaches first and emits
// packets at large publisher-side timestamps (4s in this scenario, well
// inside ffstream's split-AV arrival skew). Video publisher B then attaches
// at small timestamps. After the kernel has observed BOTH streams, the
// next audio packet from A must come out aligned to the established video
// epoch — not 4s ahead of it.
//
// Falsification: with the shipped 376cf84 fix alone (audio epoch only
// computed when isNewSource for audio — i.e. on the first packet from a
// new audio source), this test fails because that first audio packet sees
// LatestPTS==0 and the gate `LatestPTS > 0` blocks the compute; subsequent
// audio packets have setNewTimeShift=false and the gate never re-fires.
// The post-fix expectation is that the kernel re-attempts the epoch
// compute on every audio packet while audioEpochComputed is still false
// AND LatestPTS has become non-zero, so the first audio packet seen
// AFTER video establishes LatestPTS is the one that anchors the epoch.
func TestNodeKernel_AudioFirstThenVideo_Aligns(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	audioSrc := newMockPacketSource("audio-publisher")
	videoSrc := newMockPacketSource("video-publisher")

	outputCh := make(chan packetorframe.OutputUnion, 32)

	// Audio publisher A is already streaming when the kernel is created.
	// Use timestamps where the audio/LatestPTS ratio is firmly OUT of the
	// sample-rate-units range [24, 96] (sampleRate/tbDen = 48000/1000 =
	// 48), so detectAudioTimestampMismatch correctly classifies this as
	// publisher-arrival skew (not a clock-units bug) and does NOT flag
	// audioTimestampDetected — exactly the prod split-AV case.
	//
	// audioDTS=5000ms; after video sets LatestPTS=1000ms, ratio = 5,
	// which is < 24 → no rate-mismatch flag. The bug then surfaces
	// purely on the new-source/setNewTimeShift gate path.
	audio0 := audioPacket(t, audioSrc, 1 /*audio stream*/, 5000, 5000)
	out := sendPacketAndDrain(t, ctx, k, audio0, outputCh)
	assert.Equal(t, int64(5000), out.Pts(),
		"audio-only phase: no video reference yet, audio passes through unchanged")

	audio1 := audioPacket(t, audioSrc, 1, 5023, 5023)
	out = sendPacketAndDrain(t, ctx, k, audio1, outputCh)
	assert.Equal(t, int64(5023), out.Pts(),
		"audio-only phase: still no video reference, no epoch shift")

	// Now video publisher B attaches at 1s (B re-based to ~0). LatestPTS
	// becomes 1s, and the audio/video divergence ratio is 5 — out of
	// [24, 96] — so detectAudioTimestampMismatch does not flag.
	video0 := videoPacket(t, videoSrc, 0 /*video stream*/, 1000, 1000)
	_ = sendPacketAndDrain(t, ctx, k, video0, outputCh)

	require.Equal(t, time.Second, k.LatestPTS,
		"video must update LatestPTS to 1s")
	if si := k.SourceInfo[audioSrc]; si != nil {
		require.False(t, si.AudioTimestampDetected,
			"sanity: prior audio packets must not have flagged AudioTimestampDetected on the audio source's SourceInfo (LatestPTS was 0 at the time, no detection)")
	}

	// The next audio packet from A — same source, setNewTimeShift=false —
	// is the moment the post-fix kernel must align audio to video. With
	// only commit 376cf84 in place, this packet has setNewTimeShift=false
	// AND audioTimestampDetected=false, so the compute gate stays closed;
	// audio comes out at raw 5046 (4s ahead of video). Post-fix the
	// kernel must close the gap on this packet.
	audio2 := audioPacket(t, audioSrc, 1, 5046, 5046)
	out = sendPacketAndDrain(t, ctx, k, audio2, outputCh)

	gap := time.Duration(out.Pts())*time.Millisecond - k.LatestPTS
	if gap < 0 {
		gap = -gap
	}
	assert.Less(t, gap, 250*time.Millisecond,
		"audio epoch must align with video after both are present; got audioPTS=%dms, videoLatestPTS=%v, gap=%v (expected gap < 250ms)",
		out.Pts(), k.LatestPTS, gap)
}

// TestNodeKernel_PerSourceTracking_FreshSecondAudioSource verifies that
// a SECOND audio publisher (different *mockPacketSource) attaching after
// the first one is treated as a fresh epoch reference — its own first
// packet computes its own offset against the current LatestPTS, ignoring
// any offset the prior audio source contributed.
//
// Falsification: if audio epoch state were keyed solely by stream index
// (not by source), the second source's offset would inherit the first
// source's offset and a freshly-rebased second publisher would land at
// a wrong PTS. Pinning audioEpochOffset to the *source* (not just the
// stream index) prevents that smuggling.
func TestNodeKernel_PerSourceTracking_FreshSecondAudioSource(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	videoSrc := newMockPacketSource("video-publisher")
	audioSrcA := newMockPacketSource("audio-publisher-A")
	audioSrcB := newMockPacketSource("audio-publisher-B")

	outputCh := make(chan packetorframe.OutputUnion, 32)

	// Bring up video first to establish a non-zero LatestPTS at 1s.
	// Using LatestPTS >= 1s keeps the audio/LatestPTS divergence ratio
	// outside [24, 96] for the audio offsets used below — so
	// detectAudioTimestampMismatch correctly classifies these as
	// publisher-arrival skew and the test exercises the new-source gate
	// path, not the rate-correction path.
	v := videoPacket(t, videoSrc, 0, 1000, 1000)
	_ = sendPacketAndDrain(t, ctx, k, v, outputCh)
	require.Equal(t, time.Second, k.LatestPTS)

	// Audio source A attaches at 5s — divergence ratio 5/1=5, < 24 →
	// not flagged as rate-mismatch. The kernel computes A's epoch
	// offset against LatestPTS=1s on the first packet (setNewTimeShift
	// is true: A is a brand-new source for the audio stream). A's audio
	// must come out near LatestPTS, not at raw 5000.
	a0 := audioPacket(t, audioSrcA, 1, 5000, 5000)
	out := sendPacketAndDrain(t, ctx, k, a0, outputCh)
	gapA := time.Duration(out.Pts())*time.Millisecond - k.LatestPTS
	if gapA < 0 {
		gapA = -gapA
	}
	require.Less(t, gapA, 250*time.Millisecond,
		"first audio source A must align to video epoch (got audioPTS=%dms, LatestPTS=%v, gap=%v)",
		out.Pts(), k.LatestPTS, gapA)

	// Advance video so LatestPTS moves on to 2s.
	v = videoPacket(t, videoSrc, 0, 2000, 2000)
	_ = sendPacketAndDrain(t, ctx, k, v, outputCh)
	require.Equal(t, 2*time.Second, k.LatestPTS)

	// Audio source B attaches at 7s — divergence ratio 7/2=3.5, < 24 →
	// not flagged. Its first packet (setNewTimeShift=true for B because
	// B is a new source for the audio stream) must compute B's OWN
	// offset against LatestPTS=2s, NOT inherit A's offset (which
	// implicitly assumed publisher-side base ~4s).
	b0 := audioPacket(t, audioSrcB, 1, 7000, 7000)
	out = sendPacketAndDrain(t, ctx, k, b0, outputCh)
	gapB := time.Duration(out.Pts())*time.Millisecond - k.LatestPTS
	if gapB < 0 {
		gapB = -gapB
	}
	assert.Less(t, gapB, 250*time.Millisecond,
		"second audio source B must compute its own fresh epoch (got audioPTS=%dms, LatestPTS=%v, gap=%v); inheriting A's offset would put B at ~5s past LatestPTS",
		out.Pts(), k.LatestPTS, gapB)
}

// TestNodeKernel_FreshSinglePublisher_NoRegression guards the fresh-
// publisher case under ShouldFixPTS=false (the prod default for routes:
// the merged route is constructed via NewNodeKernel(ctx) with no options,
// so ShouldFixPTS defaults to false). A single publisher whose audio
// arrives at small timestamps (<2s skew) must NOT receive a spurious
// epoch shift; audio should pass through unchanged. This is the
// regression the prior subagent's sketch hit and is what currently works
// in single-publisher prod, so the fix must not break it.
func TestNodeKernel_FreshSinglePublisher_NoRegression(t *testing.T) {
	ctx := context.Background()
	// ShouldFixPTS defaults to false (NewNodeKernel(ctx) with no options).
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	src := newMockPacketSource("single-publisher")
	outputCh := make(chan packetorframe.OutputUnion, 32)

	// Video establishes LatestPTS at a small value.
	v := videoPacket(t, src, 0, 50, 50)
	_ = sendPacketAndDrain(t, ctx, k, v, outputCh)

	// Audio packet arrives at a slightly later timestamp (typical
	// fresh-publisher arrival jitter — well inside the 2s detection
	// threshold). It must come out unchanged.
	a := audioPacket(t, src, 1, 80, 80)
	out := sendPacketAndDrain(t, ctx, k, a, outputCh)
	assert.Equal(t, int64(80), out.Pts(),
		"fresh publisher with small audio/video skew: audio must pass through unchanged")
	si := k.SourceInfo[src]
	require.NotNil(t, si, "per-source bookkeeping must be initialized")
	assert.Zero(t, si.AudioEpochOffset,
		"no shift should be applied in the fresh small-skew case (got offset=%v)", si.AudioEpochOffset)
	assert.True(t, si.AudioEpochComputed,
		"the small-skew case must still be locked in (AudioEpochComputed=true) so subsequent packets do not keep recomputing")
}

// audioPacketWithRate builds an audio Input with a configurable
// CodecParameters().SampleRate and timebase, used to drive
// detectAudioTimestampMismatch into the rate-correction path or
// publisher-arrival-skew path independently per source.
func audioPacketWithRate(
	t *testing.T,
	src packet.Source,
	streamIndex int,
	dts, pts int64,
	tb astiav.Rational,
	sampleRate int,
) packet.Input {
	t.Helper()
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(streamIndex)
	pkt.SetDts(dts)
	pkt.SetPts(pts)

	stream := astiav.AllocFormatContext().NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream.CodecParameters().SetSampleRate(sampleRate)
	stream.SetTimeBase(tb)
	stream.SetIndex(streamIndex)

	info := &packet.StreamInfo{
		Stream:   stream,
		Source:   src,
		TimeBase: tb,
	}
	return packet.BuildInput(pkt, info)
}

// TestNodeKernel_PerSourceRateCorrection_NoSmuggle pins the per-source
// migration of the rate-correction state (AudioTimestampDetected,
// AudioSampleRate, AudioTimeBaseDen). Two simultaneously-attached audio
// publishers — one in genuine sample-count units (ratio matches sample
// rate / timebase), one in correct millisecond units — must each get
// their own SourceInfo state, so the rescale at the call site does NOT
// corrupt the well-formed publisher's timestamps.
//
// Falsification: if the rate-correction state is kernel-global (the pre-
// migration shape), publisher A flipping audioTimestampDetected = true
// causes publisher B's already-correct millisecond DTS to be rescaled
// by tbDen/sampleRate, collapsing B's timestamps to ~0 (since B's
// audioDTS / 48000 is a tiny fraction). The test asserts the post-
// migration invariant: B's timestamps survive A's detection.
func TestNodeKernel_PerSourceRateCorrection_NoSmuggle(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	tb := astiav.NewRational(1, 1000)
	videoSrc := newMockPacketSource("video")
	rateMismatchSrc := newMockPacketSource("audio-sample-count-units")
	wellFormedSrc := newMockPacketSource("audio-millisecond-units")

	outputCh := make(chan packetorframe.OutputUnion, 32)

	// Video establishes LatestPTS=1s so detectAudioTimestampMismatch can
	// compare against a non-zero reference.
	_ = sendPacketAndDrain(t, ctx, k,
		videoPacket(t, videoSrc, 0, 1000, 1000), outputCh)
	require.Equal(t, time.Second, k.LatestPTS)

	// Publisher A sends audio in sample-count units: 48000 samples at
	// wall-clock 1s appears as DTS=48000 in 1/1000 timebase. Ratio
	// 48000/1000=48 matches sampleRate/tbDen, so detection fires for A.
	aIn := audioPacketWithRate(t, rateMismatchSrc, 1, 48000, 48000, tb, 48000)
	aOut := sendPacketAndDrain(t, ctx, k, aIn, outputCh)
	siA := k.SourceInfo[rateMismatchSrc]
	require.NotNil(t, siA, "publisher A must have its own SourceInfo")
	require.True(t, siA.AudioTimestampDetected,
		"publisher A in sample-count units must trigger per-source detection")
	require.Equal(t, int64(48000), siA.AudioSampleRate)
	require.Equal(t, int64(1000), siA.AudioTimeBaseDen)
	// A's DTS=48000 is rescaled to 48000*1000/48000=1000ms — same as
	// LatestPTS — so the epoch compute records a zero offset (small-skew
	// branch under audioEpochAlignmentThreshold). A's output PTS lands
	// at ~1000.
	assert.InDelta(t, int64(1000), aOut.Pts(), 5,
		"A's rescaled PTS must land at ~LatestPTS (1000); got %d", aOut.Pts())

	// Publisher B sends audio in correct millisecond units: DTS=1100 at
	// wall-clock 1.1s. Ratio 1100/1000=1.1, far below 24, so detection
	// for B sees publisher-arrival-skew (or under-threshold) and does
	// NOT flag rate-correction. Critically: even though A's SourceInfo
	// has AudioSampleRate=48000, B's separate SourceInfo must not.
	bIn := audioPacketWithRate(t, wellFormedSrc, 2, 1100, 1100, tb, 48000)
	bOut := sendPacketAndDrain(t, ctx, k, bIn, outputCh)
	siB := k.SourceInfo[wellFormedSrc]
	require.NotNil(t, siB, "publisher B must have its own SourceInfo")

	// Pre-migration (kernel-global state): siA's flag would have been
	// kernel-global → B's DTS=1100 would have been rescaled to
	// 1100*1000/48000 = 22 → collapse. Post-migration: B's SourceInfo
	// is independent and zero — no rescale.
	assert.False(t, siB.AudioTimestampDetected,
		"publisher B's per-source AudioTimestampDetected must NOT inherit from publisher A; B is in millisecond units")
	assert.Zero(t, siB.AudioSampleRate,
		"publisher B's AudioSampleRate must remain 0 (no rate correction needed)")

	// B's output PTS must be ~1100 (no rescale). Pre-migration this
	// would have been ~22 — kernel-global state from A would have
	// rescaled B by 1000/48000.
	assert.InDelta(t, int64(1100), bOut.Pts(), 5,
		"B's PTS must NOT be rescaled by A's rate-correction state; got %d, expected ~1100",
		bOut.Pts())
}
