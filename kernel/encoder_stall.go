// encoder_stall.go isolates the av1_mediacodec / silent-consume stall
// watchdog state and recovery path from the core encoder plumbing in
// encoder.go. Two complementary detectors converge on the same Reinit:
//
//   handleEncoderStall (EAGAIN-loop class)
//     SendFrame returns EAGAIN, Drain produces 0 packets,
//     consecutiveStalls increments per failed frame.
//
//   handleSilentConsumeStall (silent-consume class)
//     SendFrame returns nil, Drain produces 0 packets,
//     framesInSinceLastPacket increments per accepted frame.
//
// Both call escalateStallReinit when their threshold trips, which
// performs codec.EncoderReiniter.Reinit + clears all watchdog state in
// a single canonical order. Keeping the recovery in one helper means
// the post-Reinit invariants cannot drift between the two paths.

package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/logger"
)

// handleEncoderStall implements the av1_mediacodec stall watchdog:
// when sendFrameWithDrainRetry has bailed with errEncoderStalled,
// decide whether to drop the frame quietly or escalate to a full
// Reinit of the codec.
//
// The decision uses streamEncoder.consecutiveStalls (already bumped
// inside the bounded retry helper). Once the count crosses
// encoderStallReinitThreshold and the encoder implements
// codec.EncoderReiniter, we Reinit and clear the watchdog state via
// escalateStallReinit — see that helper for the post-Reinit invariants.
//
// All errors other than errEncoderStalled propagate unchanged.
func (e *Encoder[EF]) handleEncoderStall(
	ctx context.Context,
	streamEncoder *streamEncoder,
	err error,
) error {
	if err == nil {
		// Even on a clean SendFrame path, we must check the
		// silent-consume watchdog: SendFrame returning nil while Drain
		// emits nothing is a stall mode. The check is a no-op below
		// the threshold.
		return e.handleSilentConsumeStall(ctx, streamEncoder)
	}
	if !errors.Is(err, errEncoderStalled) {
		return err
	}
	stalls := streamEncoder.consecutiveStalls.Load()
	if stalls < encoderStallReinitThreshold {
		// Below threshold: just drop the frame, no Reinit yet.
		return nil
	}
	r, ok := streamEncoder.Encoder.(codec.EncoderReiniter)
	if !ok {
		// Encoder cannot Reinit (e.g. dummy copy/raw). Nothing more
		// we can do; surface the stall by dropping the frame.
		logger.Warnf(ctx, "encoder stalled %d times but encoder does not implement EncoderReiniter; dropping frame", stalls)
		return nil
	}
	return escalateStallReinit(
		ctx, streamEncoder, r,
		fmt.Sprintf("encoder stalled %d times", stalls),
		"reinit after stall failed",
	)
}

// handleSilentConsumeStall implements the silent-consume watchdog:
// when SendFrame keeps returning nil but the drain callback emits no
// packets, streamEncoder.framesInSinceLastPacket grows unbounded while
// output is frozen. Once it crosses encoderSilentConsumeThreshold we
// escalate to a full Reinit on the same path as the EAGAIN-stall
// watchdog.
//
// This is orthogonal to handleEncoderStall (EAGAIN-loop class):
//   - EAGAIN-stall: SendFrame fails with EAGAIN, Drain produces nothing
//     -> consecutiveStalls increments per failed frame.
//   - silent-consume: SendFrame succeeds, Drain produces nothing
//     -> framesInSinceLastPacket increments per accepted frame.
//
// Both routes converge on the same Reinit + state-clear recovery via
// escalateStallReinit.
func (e *Encoder[EF]) handleSilentConsumeStall(
	ctx context.Context,
	streamEncoder *streamEncoder,
) error {
	frames := streamEncoder.framesInSinceLastPacket.Load()
	if frames < encoderSilentConsumeThreshold {
		return nil
	}
	r, ok := streamEncoder.Encoder.(codec.EncoderReiniter)
	if !ok {
		// No Reinit capability: log once-per-threshold and reset the
		// counter so we don't spam. Without the reset every subsequent
		// SendFrame would re-trigger the same warning.
		logger.Warnf(ctx, "encoder silent-consume stall (%d frames in, 0 packets out) but encoder does not implement EncoderReiniter; dropping watchdog signal", frames)
		streamEncoder.framesInSinceLastPacket.Store(0)
		return nil
	}
	return escalateStallReinit(
		ctx, streamEncoder, r,
		fmt.Sprintf("encoder silent-consume stall: %d frames in, 0 packets out", frames),
		"reinit after silent-consume stall failed",
	)
}

// escalateStallReinit performs a codec.EncoderReiniter.Reinit and clears
// all watchdog state for the affected streamEncoder. Both stall paths
// (EAGAIN-loop / handleEncoderStall, silent-consume /
// handleSilentConsumeStall) converge here, so the post-Reinit
// invariants are guaranteed identical regardless of which detector
// triggered the recovery:
//
//  1. consecutiveStalls reset — a freshly Reinit'd codec starts with
//     zero consecutive stalls.
//  2. FrameInfoFIFO cleared — stale FIFO entries belong to the dead
//     codec instance; without this clear, e.drain's FIFO-pop would tag
//     packets emitted by the freshly opened MediaCodec with old
//     timestamps.
//  3. framesInSinceLastPacket reset — the silent-consume counter must
//     also clear: a freshly Reinit'd codec starts with zero
//     accepted-but-unflushed frames.
//  4. audioResampleNextOutPTS / audioResampleHaveOutAnchor reset — the
//     resample-PTS anchor (set in fitFrameForEncoding's audio
//     resample-restamp branch) is bound to the pre-Reinit codec's
//     sample timeline. After the codec is reopened fresh, leaving the
//     anchor in place would stamp post-Reinit output frames with PTS
//     values derived from the dead codec's accumulated output-sample
//     counter, producing a discontinuity in the audio timeline
//     (desync after reconnect). The reset is unconditional because
//     streamEncoder lifecycle is per-stream
//     (one streamEncoder per streamIndex in Encoder.encoders, populated
//     by initEncoderFor) — a video Reinit hits a streamEncoder whose
//     audio anchor fields are zero-valued and never read, so the reset
//     is a no-op there.
//
// All four resets are atomic-on-success: if r.Reinit(ctx) fails the
// function returns the wrapped error before any reset, leaving the
// watchdog state untouched (a half-reset on a still-dead codec would
// lie about recovery and a subsequent Send/Drain pass would observe an
// inconsistent mix of "fresh counters" plus "stale FIFO entries from
// before the Reinit attempt"). On Reinit success the four resets land
// together — counters bracket the FIFO clear so that any code path
// reading the watchdog state post-helper observes a fully canonical
// "fresh codec" snapshot rather than a transitional state where, say,
// consecutiveStalls=0 but FrameInfoFIFO still holds entries from the
// dead instance.
//
// Both watchdog call sites (handleSilentConsumeStall and the
// consecutive-stall escalation in handleEncoderStall) funnel through
// this helper precisely so the post-Reinit state can never diverge
// between the two stall classes again — the pre-helper layout reset
// the same fields in subtly different orders across the two paths.
//
// `triggerMsg` is the pre-Reinit Errorf log line ("encoder stalled N
// times" / "silent-consume stall: N frames in") and `reinitErrPrefix`
// is the wrap prefix on a Reinit failure. These are pulled out so the
// logging stays distinct between the two stall classes (operators must
// be able to grep for the exact stall signature).
func escalateStallReinit(
	ctx context.Context,
	se *streamEncoder,
	r codec.EncoderReiniter,
	triggerMsg string,
	reinitErrPrefix string,
) error {
	logger.Errorf(ctx, "%s; reinit", triggerMsg)
	if rerr := r.Reinit(ctx); rerr != nil {
		return fmt.Errorf("%s: %w", reinitErrPrefix, rerr)
	}
	se.consecutiveStalls.Store(0)
	se.FrameInfoFIFO = se.FrameInfoFIFO[:0]
	se.framesInSinceLastPacket.Store(0)
	se.audioResampleNextOutPTS = 0
	se.audioResampleHaveOutAnchor = false
	return nil
}
