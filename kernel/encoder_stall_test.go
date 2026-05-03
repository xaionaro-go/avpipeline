// encoder_stall_test.go covers the av1_mediacodec genuine stall
// watchdog. Two patches under test:
//
//   sendFrameWithDrainRetry: replace the previous unbounded
//   `for { SendFrame; if EAGAIN { drain; continue } }` with a bounded
//   form (sendFrameMaxRetries cycles per fitted frame). On exhausted
//   retries the loop bumps streamEncoder.consecutiveStalls and returns
//   errEncoderStalled instead of spinning forever.
//
//   handleEncoderStall: when the bounded loop bails, the call site
//   escalates to codec.EncoderReiniter.Reinit once the consecutive-stall
//   count crosses encoderStallReinitThreshold. After a successful Reinit
//   the FIFO is cleared (otherwise stale entries from the dead codec
//   instance would mismatch packets emitted by the freshly opened
//   MediaCodec).
//
// Determinism: pure in-memory state machine; no real codec, no
// goroutines, no real-clock dependencies.

package kernel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

// stallTestEncoder is a minimal codec.Encoder mock for the stall
// watchdog tests. Embedding codec.EncoderRaw satisfies the full
// interface with no-op semantics; we override only what the watchdog
// path inspects (MediaType + Reinit). SendFrame / drain behavior is
// driven by the test directly via sendFrameWithDrainRetry's function-
// typed dependencies, so the mock does not need a real SendFrame.
type stallTestEncoder struct {
	codec.EncoderRaw
	reinitCalls atomic.Int64
}

var _ codec.Encoder = (*stallTestEncoder)(nil)
var _ codec.EncoderReiniter = (*stallTestEncoder)(nil)

func (e *stallTestEncoder) MediaType(context.Context) astiav.MediaType {
	return astiav.MediaTypeVideo
}

func (e *stallTestEncoder) Reinit(context.Context) error {
	e.reinitCalls.Add(1)
	return nil
}

// TestEncoder_StallBoundsRetryLoop is the RED test for the bounded
// retry loop. With a SendFrame that
// always returns EAGAIN and a drain that emits zero packets (no
// progress), the loop must exit after sendFrameMaxRetries+1 SendFrame
// calls and return errEncoderStalled. Falsification: revert to the
// original `for {}` form and this test loops forever (or hits the
// test's outer timeout).
func TestEncoder_StallBoundsRetryLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	var sendCalls int
	sendFrame := func(context.Context, *astiav.Frame) error {
		sendCalls++
		return astiav.ErrEagain
	}
	// Drain emits no packets — drainPacketCount stays at 0 across
	// every call. This is the av1_mediacodec stall pattern.
	drain := func(context.Context) error {
		return nil
	}

	err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{}, astiav.MediaTypeVideo, sendFrame, drain)

	require.ErrorIs(t, err, errEncoderStalled,
		"bounded retry loop must surface errEncoderStalled when SendFrame stalls")
	require.LessOrEqual(t, sendCalls, sendFrameMaxRetries+1,
		"SendFrame must be invoked at most sendFrameMaxRetries+1 times per fitted frame (got %d)", sendCalls)
	require.Equal(t, sendFrameMaxRetries+1, sendCalls,
		"SendFrame must be invoked exactly sendFrameMaxRetries+1 times in the persistent-EAGAIN path (got %d)", sendCalls)
	require.Equal(t, uint64(1), se.consecutiveStalls.Load(),
		"consecutiveStalls must increment exactly once per bailed frame")
	require.Empty(t, se.FrameInfoFIFO,
		"FrameInfoFIFO must not grow when SendFrame never accepts the frame")
}

// TestEncoder_StallProgressResetsRetryBudget verifies the helper does
// not false-stall a healthy "needs draining every frame" codec: when
// Drain produces packets, the local retry budget resets and the loop
// keeps trying. This is the AAC-with-encoder-delay pattern, which
// must keep working after the bound was added.
func TestEncoder_StallProgressResetsRetryBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	// Pattern: SendFrame returns EAGAIN exactly 5 times (each followed
	// by a Drain that emits 1 packet → progress), then accepts.
	// Without the retries=0 reset on progress, this would bail at
	// retry #2; with the reset it must succeed.
	var sendCalls int
	sendFrame := func(context.Context, *astiav.Frame) error {
		sendCalls++
		if sendCalls <= 5 {
			return astiav.ErrEagain
		}
		return nil
	}
	drain := func(context.Context) error {
		se.drainPacketCount.Add(1)
		return nil
	}

	err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{}, astiav.MediaTypeVideo, sendFrame, drain)
	require.NoError(t, err, "loop must keep retrying while Drain shows progress")
	require.Equal(t, 6, sendCalls, "5 EAGAIN + 1 success")
	require.Len(t, se.FrameInfoFIFO, 1, "successful SendFrame must push exactly one FIFO entry")
	require.Equal(t, uint64(0), se.consecutiveStalls.Load(),
		"a successful SendFrame must reset consecutiveStalls")
}

// TestEncoder_StallDrainErrorPropagates verifies non-EAGAIN drain
// errors surface to the caller (regression guard for fmt.Errorf wrap).
func TestEncoder_StallDrainErrorPropagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	drainErr := errors.New("disk full")
	sendFrame := func(context.Context, *astiav.Frame) error { return astiav.ErrEagain }
	drain := func(context.Context) error { return drainErr }

	err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{}, astiav.MediaTypeVideo, sendFrame, drain)
	require.ErrorIs(t, err, drainErr)
}

// TestEncoder_StallSendFrameErrorPropagates verifies non-EAGAIN
// SendFrame errors are wrapped and surfaced (regression guard).
func TestEncoder_StallSendFrameErrorPropagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	sendErr := errors.New("codec broken")
	sendFrame := func(context.Context, *astiav.Frame) error { return sendErr }
	drain := func(context.Context) error { return nil }

	err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{}, astiav.MediaTypeVideo, sendFrame, drain)
	require.ErrorIs(t, err, sendErr)
	require.NotErrorIs(t, err, errEncoderStalled,
		"plain non-EAGAIN errors must not be conflated with the stall sentinel")
}

// TestEncoderReinit_OnStallThreshold: after
// encoderStallReinitThreshold consecutive stalled frames the wrapping
// caller invokes codec.EncoderReiniter.Reinit exactly once, resets the
// stall counter, and clears FrameInfoFIFO.
func TestEncoderReinit_OnStallThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	// Pre-populate FIFO with stale entries that belong to the
	// (about-to-be-replaced) codec instance. The watchdog must wipe them.
	se.FrameInfoFIFO = []FrameInfo{{PTS: 1}, {PTS: 2}, {PTS: 3}}

	e := &Encoder[codec.EncoderFactory]{}

	// Sub-threshold: handleEncoderStall must drop the frame quietly,
	// NOT call Reinit, NOT clear the FIFO.
	for i := uint64(1); i < encoderStallReinitThreshold; i++ {
		se.consecutiveStalls.Store(i)
		err := e.handleEncoderStall(ctx, se, errEncoderStalled)
		require.NoError(t, err, "sub-threshold stall must be dropped silently")
		require.Equal(t, int64(0), enc.reinitCalls.Load(),
			"Reinit must not fire below threshold (stalls=%d)", i)
		require.Len(t, se.FrameInfoFIFO, 3,
			"FIFO must not be cleared below threshold (stalls=%d)", i)
	}

	// At threshold: Reinit fires once, FIFO clears, counter resets.
	se.consecutiveStalls.Store(encoderStallReinitThreshold)
	err := e.handleEncoderStall(ctx, se, errEncoderStalled)
	require.NoError(t, err, "Reinit success path must swallow errEncoderStalled")
	require.Equal(t, int64(1), enc.reinitCalls.Load(),
		"Reinit must fire exactly once at threshold")
	require.Equal(t, uint64(0), se.consecutiveStalls.Load(),
		"consecutiveStalls must reset after a successful Reinit")
	require.Empty(t, se.FrameInfoFIFO,
		"FrameInfoFIFO must be cleared after Reinit (stale entries would mismatch packets from the new codec instance)")

	// Post-Reinit: a fresh stall must not retrigger Reinit until the
	// threshold is reached again — proves the counter reset above is
	// honored.
	se.consecutiveStalls.Store(1)
	err = e.handleEncoderStall(ctx, se, errEncoderStalled)
	require.NoError(t, err)
	require.Equal(t, int64(1), enc.reinitCalls.Load(),
		"Reinit must not fire on the very next stall after a successful reinit cycle")
}

// TestEncoderReinit_NonStallErrorPassthrough ensures handleEncoderStall
// does not interfere with errors that are NOT errEncoderStalled.
func TestEncoderReinit_NonStallErrorPassthrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	// Counter is at threshold but the error is unrelated -- must NOT
	// consume the threshold or trigger Reinit.
	se.consecutiveStalls.Store(encoderStallReinitThreshold)

	e := &Encoder[codec.EncoderFactory]{}
	other := errors.New("disk full")
	err := e.handleEncoderStall(ctx, se, other)
	require.ErrorIs(t, err, other, "non-stall errors must propagate unchanged")
	require.Equal(t, int64(0), enc.reinitCalls.Load(), "Reinit must not fire for non-stall errors")
}

// TestEncoderReinit_NoReiniterCapability covers the case where the
// underlying encoder does not implement EncoderReiniter (e.g. a pure
// copy/raw encoder). handleEncoderStall must drop the frame and
// surface no error, since there is no recovery action available.
func TestEncoderReinit_NoReiniterCapability(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// codec.EncoderRaw does not implement EncoderReiniter on its own
	// (it has no codec context to reopen).
	se := &streamEncoder{Encoder: codec.EncoderRaw{}}
	se.consecutiveStalls.Store(encoderStallReinitThreshold)

	e := &Encoder[codec.EncoderFactory]{}
	err := e.handleEncoderStall(ctx, se, errEncoderStalled)
	require.NoError(t, err, "stall on a non-reiniter encoder must be dropped, not propagated")
}

// TestEncoder_StallFIFODoesNotGrowUnboundedly is the resource-leak
// guard. On a real stall:
//   - failed SendFrames do NOT push to the FIFO (no pretend-success)
//   - on Reinit the FIFO is cleared
// We feed many frames through a perma-stalled mock and assert the
// FIFO never grows.
func TestEncoder_StallFIFODoesNotGrowUnboundedly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)
	sendFrame := func(context.Context, *astiav.Frame) error { return astiav.ErrEagain }
	drain := func(context.Context) error { return nil }

	const frames = 50
	for i := 0; i < frames; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{}, astiav.MediaTypeVideo, sendFrame, drain)
		require.ErrorIs(t, err, errEncoderStalled)
	}
	require.Empty(t, se.FrameInfoFIFO,
		"FIFO must remain empty across many stalled frames (got %d entries after %d frames)",
		len(se.FrameInfoFIFO), frames)
	require.Equal(t, uint64(frames), se.consecutiveStalls.Load(),
		"consecutiveStalls must increment per stalled frame")
}

// TestEncoder_StallPostReinitFlow simulates the recovery flow: after
// the bound bails three times and the watchdog runs Reinit, the next
// SendFrame succeeds and frames flow again.
func TestEncoder_StallPostReinitFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	e := &Encoder[codec.EncoderFactory]{}

	// Phase 1: stall threshold-many frames in a row.
	stallSendFrame := func(context.Context, *astiav.Frame) error { return astiav.ErrEagain }
	drain := func(context.Context) error { return nil }
	for i := 0; i < encoderStallReinitThreshold; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(i)}, astiav.MediaTypeVideo, stallSendFrame, drain)
		require.ErrorIs(t, err, errEncoderStalled)
		err = e.handleEncoderStall(ctx, se, err)
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), enc.reinitCalls.Load(),
		"Reinit must fire exactly once across threshold-many stalls")
	require.Empty(t, se.FrameInfoFIFO, "FIFO must be empty post-Reinit")

	// Phase 2: post-Reinit, SendFrame works; frames flow.
	healthySendFrame := func(context.Context, *astiav.Frame) error { return nil }
	for i := 0; i < 5; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(100 + i)}, astiav.MediaTypeVideo, healthySendFrame, drain)
		require.NoError(t, err)
	}
	require.Len(t, se.FrameInfoFIFO, 5, "post-Reinit frames must push to FIFO normally")
	require.Equal(t, uint64(0), se.consecutiveStalls.Load(),
		"consecutiveStalls must remain 0 once SendFrame is healthy again")
}

// --- silent-consume watchdog ----------------------------------------
//
// Distinct from the EAGAIN-loop class. Failure mode: SendFrame keeps
// returning nil (codec accepts every frame) but the drain callback emits
// no packets — observed across 13+ minutes of frozen output on
// av1_mediacodec on a Pixel device. The EAGAIN-loop watchdog never fires
// because no EAGAIN ever returns; without a separate watchdog the encoder
// silently buffers frames forever.

// TestEncoder_SilentConsumeStallTriggersReinit feeds 100 frames through a
// SendFrame that always succeeds and a Drain that never emits. After
// encoderSilentConsumeThreshold accepted frames, handleEncoderStall must
// invoke Reinit at least once, reset framesInSinceLastPacket, and clear
// FrameInfoFIFO.
//
// Falsification: comment out the threshold check inside
// handleSilentConsumeStall — Reinit count drops to 0 and this test fails.
func TestEncoder_SilentConsumeStallTriggersReinit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	e := &Encoder[codec.EncoderFactory]{}

	// SendFrame always accepts; Drain never emits a packet.
	healthySendFrame := func(context.Context, *astiav.Frame) error { return nil }
	silentDrain := func(context.Context) error { return nil }

	const totalFrames = 100
	for i := 0; i < totalFrames; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(i)}, astiav.MediaTypeVideo, healthySendFrame, silentDrain)
		require.NoError(t, err, "SendFrame mock returns nil; helper must not error")
		// Mirror the real call site: handleEncoderStall is invoked with
		// the (nil) result, which routes to the silent-consume watchdog.
		err = e.handleEncoderStall(ctx, se, nil)
		require.NoError(t, err)
	}

	require.GreaterOrEqual(t, enc.reinitCalls.Load(), int64(1),
		"silent-consume watchdog must fire Reinit at least once across %d frames", totalFrames)
	require.Less(t, se.framesInSinceLastPacket.Load(), uint64(encoderSilentConsumeThreshold),
		"counter must be below threshold after a Reinit (got %d)",
		se.framesInSinceLastPacket.Load())
	// FIFO clears at each Reinit; between reinits it grows by one per
	// frame. Final state depends on (totalFrames mod threshold). The
	// invariant we care about is: the FIFO is bounded by the threshold,
	// not unbounded.
	require.LessOrEqual(t, len(se.FrameInfoFIFO), encoderSilentConsumeThreshold,
		"FIFO must stay bounded by the silent-consume threshold (got %d entries)",
		len(se.FrameInfoFIFO))
}

// TestEncoder_SilentConsumeWithProgressDoesNotReinit is the no-false-trigger
// guard. SendFrame always accepts; Drain emits one packet on every call —
// i.e. the codec is healthy. framesInSinceLastPacket must be reset on
// every drain emission, so the watchdog never fires.
//
// Falsification: remove the framesInSinceLastPacket.Store(0) reset in the
// drain callback. The counter would grow monotonically and Reinit would
// fire after the threshold, failing the assertion below.
func TestEncoder_SilentConsumeWithProgressDoesNotReinit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	e := &Encoder[codec.EncoderFactory]{}

	healthySendFrame := func(context.Context, *astiav.Frame) error { return nil }
	// Drain emits one packet — i.e. mirrors what the real drain
	// callback does on every successful packet receive: reset the
	// silent-consume counter.
	healthyDrain := func(context.Context) error {
		se.drainPacketCount.Add(1)
		se.framesInSinceLastPacket.Store(0)
		return nil
	}

	// Run well past the threshold to prove the counter resets keep
	// firing.
	const totalFrames = encoderSilentConsumeThreshold * 3
	for i := 0; i < totalFrames; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(i)}, astiav.MediaTypeVideo, healthySendFrame, healthyDrain)
		require.NoError(t, err)
		// Simulate the real drain firing after each successful
		// SendFrame (the encoder sendFrame() does this in the per-frame
		// loop's drain call).
		require.NoError(t, healthyDrain(ctx))
		err = e.handleEncoderStall(ctx, se, nil)
		require.NoError(t, err)
	}

	require.Equal(t, int64(0), enc.reinitCalls.Load(),
		"healthy progress must never trigger silent-consume Reinit")
	require.Equal(t, uint64(0), se.framesInSinceLastPacket.Load(),
		"framesInSinceLastPacket must remain at 0 in the healthy-drain pattern")
}

// TestEncoder_SilentConsumeAndEAGAIN_BothPaths exercises both watchdog
// classes in sequence on the same streamEncoder, asserting the two
// watchdogs are orthogonal (each fires on its own counter, neither
// steals the other's signal) and share the same Reinit + FIFO-clear
// recovery.
func TestEncoder_SilentConsumeAndEAGAIN_BothPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)

	e := &Encoder[codec.EncoderFactory]{}

	silentDrain := func(context.Context) error { return nil }

	// --- Phase 1: EAGAIN-stall ---
	stallSendFrame := func(context.Context, *astiav.Frame) error { return astiav.ErrEagain }
	for i := 0; i < encoderStallReinitThreshold; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(i)}, astiav.MediaTypeVideo, stallSendFrame, silentDrain)
		require.ErrorIs(t, err, errEncoderStalled, "Phase 1 must surface stall sentinel")
		err = e.handleEncoderStall(ctx, se, err)
		require.NoError(t, err)
	}
	reinitsAfterPhase1 := enc.reinitCalls.Load()
	require.Equal(t, int64(1), reinitsAfterPhase1,
		"Phase 1 (EAGAIN-stall) must trigger exactly one Reinit")
	require.Equal(t, uint64(0), se.framesInSinceLastPacket.Load(),
		"silent-consume counter must reset after an EAGAIN-path Reinit")

	// --- Phase 2: silent-consume ---
	healthySendFrame := func(context.Context, *astiav.Frame) error { return nil }
	for i := 0; i < encoderSilentConsumeThreshold+5; i++ {
		err := sendFrameWithDrainRetry(ctx, se, frame, FrameInfo{PTS: int64(1000 + i)}, astiav.MediaTypeVideo, healthySendFrame, silentDrain)
		require.NoError(t, err, "Phase 2 SendFrame mock returns nil")
		err = e.handleEncoderStall(ctx, se, nil)
		require.NoError(t, err)
	}
	require.Greater(t, enc.reinitCalls.Load(), reinitsAfterPhase1,
		"Phase 2 (silent-consume) must trigger at least one additional Reinit (orthogonal to Phase 1's EAGAIN counter)")
	require.Equal(t, uint64(0), se.consecutiveStalls.Load(),
		"silent-consume Reinit must also clear the EAGAIN counter (shared recovery)")
}

// TestEncoder_SilentConsumeNoReiniterCapability verifies the watchdog
// degrades gracefully when the encoder cannot Reinit: it must reset the
// counter (so we don't log every subsequent frame) and surface no error.
func TestEncoder_SilentConsumeNoReiniterCapability(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{Encoder: codec.EncoderRaw{}}
	se.framesInSinceLastPacket.Store(encoderSilentConsumeThreshold)

	e := &Encoder[codec.EncoderFactory]{}
	err := e.handleEncoderStall(ctx, se, nil)
	require.NoError(t, err, "no-reiniter path must drop the watchdog signal silently")
	require.Equal(t, uint64(0), se.framesInSinceLastPacket.Load(),
		"counter must be reset so the warning isn't spammed every subsequent frame")
}

// TestEncoder_SilentConsumeBelowThresholdNoReinit guards the threshold
// boundary: framesInSinceLastPacket = threshold-1 must NOT trigger
// Reinit. Off-by-one here would cause spurious recoveries on legitimate
// codec startup delay (e.g. B-frame look-ahead).
func TestEncoder_SilentConsumeBelowThresholdNoReinit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := &stallTestEncoder{}
	se := &streamEncoder{Encoder: enc}
	se.framesInSinceLastPacket.Store(encoderSilentConsumeThreshold - 1)

	e := &Encoder[codec.EncoderFactory]{}
	err := e.handleEncoderStall(ctx, se, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), enc.reinitCalls.Load(),
		"Reinit must not fire below the silent-consume threshold")
	require.Equal(t, uint64(encoderSilentConsumeThreshold-1), se.framesInSinceLastPacket.Load(),
		"sub-threshold counter must NOT be reset by handleEncoderStall (only Reinit resets it)")
}

// TestEscalateStallReinit_ResetsAudioResampleAnchor binds the
// invariant: the audio resample-PTS anchor is bound to the pre-Reinit
// codec's accumulated output-sample counter. When escalateStallReinit
// reopens the codec via EncoderReiniter.Reinit the anchor must be
// cleared, otherwise the next resampled audio frame gets stamped with
// a PTS derived from the dead codec's counter, producing an
// audio-timeline discontinuity.
//
// Both stall classes (EAGAIN-loop and silent-consume) funnel through
// escalateStallReinit, so we cover both via the same helper here.
func TestEscalateStallReinit_ResetsAudioResampleAnchor(t *testing.T) {
	t.Parallel()

	// Sub-test "stall path": EAGAIN-class escalation clears the anchor.
	t.Run("stall path", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		enc := &stallTestEncoder{}
		se := &streamEncoder{Encoder: enc}
		// Simulate the in-flight audio resampler having already accepted
		// at least one frame: the anchor is set, the next-out PTS has
		// advanced. These are exactly the values
		// encoder.go:1156-1173 leaves behind after one successful audio
		// frame at SampleRate=48000.
		se.audioResampleHaveOutAnchor = true
		se.audioResampleNextOutPTS = 1024
		require.True(t, se.audioResampleHaveOutAnchor,
			"precondition: anchor is set before Reinit")
		require.NotEqual(t, int64(0), se.audioResampleNextOutPTS,
			"precondition: next-out PTS has advanced before Reinit")

		// Drive the EAGAIN-class escalation: counter at threshold +
		// errEncoderStalled error.
		se.consecutiveStalls.Store(encoderStallReinitThreshold)
		e := &Encoder[codec.EncoderFactory]{}
		require.NoError(t, e.handleEncoderStall(ctx, se, errEncoderStalled))

		require.Equal(t, int64(1), enc.reinitCalls.Load(),
			"Reinit must fire on the EAGAIN-stall path")
		require.False(t, se.audioResampleHaveOutAnchor,
			"audioResampleHaveOutAnchor must be cleared post-Reinit (otherwise post-Reinit audio frames inherit the dead codec's PTS counter)")
		require.Equal(t, int64(0), se.audioResampleNextOutPTS,
			"audioResampleNextOutPTS must be reset to 0 post-Reinit (otherwise next resampled frame is stamped from a stale offset)")
	})

	// Sub-test "silent-consume path": same invariant via the
	// silent-consume watchdog escalation.
	t.Run("silent-consume path", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		enc := &stallTestEncoder{}
		se := &streamEncoder{Encoder: enc}
		se.audioResampleHaveOutAnchor = true
		se.audioResampleNextOutPTS = 9600
		require.True(t, se.audioResampleHaveOutAnchor)
		require.NotEqual(t, int64(0), se.audioResampleNextOutPTS)

		// Drive the silent-consume escalation: counter at threshold +
		// nil SendFrame error (the success path that routes to the
		// silent-consume watchdog).
		se.framesInSinceLastPacket.Store(encoderSilentConsumeThreshold)
		e := &Encoder[codec.EncoderFactory]{}
		require.NoError(t, e.handleEncoderStall(ctx, se, nil))

		require.Equal(t, int64(1), enc.reinitCalls.Load(),
			"Reinit must fire on the silent-consume path")
		require.False(t, se.audioResampleHaveOutAnchor,
			"audioResampleHaveOutAnchor must be cleared post-Reinit (silent-consume path)")
		require.Equal(t, int64(0), se.audioResampleNextOutPTS,
			"audioResampleNextOutPTS must be reset to 0 post-Reinit (silent-consume path)")
	})

	// Sub-test "Reinit failure leaves anchor untouched": atomic-on-success
	// invariant — if r.Reinit fails, the anchor must NOT be cleared, so
	// the dying codec keeps stamping consistently with itself until a
	// later successful Reinit lands the four watchdog resets together.
	t.Run("reinit failure preserves anchor", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		enc := &failingReinitEncoder{}
		se := &streamEncoder{Encoder: enc}
		se.audioResampleHaveOutAnchor = true
		se.audioResampleNextOutPTS = 4242
		se.consecutiveStalls.Store(encoderStallReinitThreshold)

		e := &Encoder[codec.EncoderFactory]{}
		err := e.handleEncoderStall(ctx, se, errEncoderStalled)
		require.Error(t, err, "handleEncoderStall must surface the wrapped Reinit error")

		// All four watchdog fields must be unchanged: atomic-on-success.
		require.True(t, se.audioResampleHaveOutAnchor,
			"audioResampleHaveOutAnchor must NOT be cleared when Reinit fails")
		require.Equal(t, int64(4242), se.audioResampleNextOutPTS,
			"audioResampleNextOutPTS must NOT be reset when Reinit fails")
		require.Equal(t, uint64(encoderStallReinitThreshold), se.consecutiveStalls.Load(),
			"consecutiveStalls must NOT be reset when Reinit fails")
	})
}

// failingReinitEncoder is a codec.Encoder mock whose Reinit always
// errors. Used to exercise the atomic-on-success branch of
// escalateStallReinit (the watchdog resets must not run on a failed
// Reinit).
type failingReinitEncoder struct {
	codec.EncoderRaw
}

var _ codec.EncoderReiniter = (*failingReinitEncoder)(nil)

func (e *failingReinitEncoder) Reinit(context.Context) error {
	return errors.New("simulated reinit failure")
}
