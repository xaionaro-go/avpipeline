// raw_frame_source_pixfmt.go injects pix_fmt=yuv420p into MediaCodec encoder
// open-time options when the upstream pipeline supplies decoded frames
// directly (no upstream decoder).
//
// Why this is needed: avpipeline/codec/codec.go:setupPixelFormat defaults
// MediaCodec encoders to AV_PIX_FMT_MEDIACODEC when a HW device context is
// reusable, which makes mediacodecenc.c open with pix_fmt=MEDIACODEC and
// take its Surface-passthrough branch (mediacodecenc.c:468). On the
// camera-input path there is no upstream decoder, so no Surface is
// attached as frame->data[3]; mediacodec_send then returns 0 silently
// (mediacodecenc.c:802-810) and frames are silently consumed without
// producing packets — the visible silent-consume bug. Forcing
// pix_fmt=yuv420p short-circuits the default and steers mediacodecenc onto
// the SW-upload encode path (copy_frame_to_buffer at line 827), which
// works without a Surface. yuv420p is deliberately planar: Android camera
// rawvideo can be YUV420P, NV12, or NV21, and choosing NV12 as the encoder
// upload format makes NV21 input vulnerable to U/V-plane interpretation
// mistakes if it reaches the upload copy path without conversion.
//
// yuv420p is verified supported by all *_mediacodec encoders in
// libavcodec/mediacodecenc.c via the shared avc_pix_fmts[] = {MEDIACODEC,
// YUV420P, NV12} array referenced by every DECLARE_MEDIACODEC_ENCODER.

package streammux

import (
	"context"
	"strings"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

const (
	rawFrameSourceMediaCodecPixFmt = "yuv420p"
)

// SetRawFrameSource flips the StreamMux-level RawFrameSource flag on. If
// the flag transitions from false to true, every existing Output's
// encoder factory is retroactively patched in place to inject
// pix_fmt=yuv420p (when MediaCodec). This is the integration point for
// callers (ffstream) that learn about a raw-frame upstream AFTER the
// first SwitchOutputByProps has already created an Output and
// reconfigured its encoder factory — wingout's gRPC AddInput hot-add of
// android_camera at priority 0 after Start is the production trigger.
//
// false→true is the only direction that does work; once a raw-frame
// source has been seen the flag stays sticky-true to keep the
// open-encoder pix_fmt consistent (the encoder is opened lazily on the
// first frame, and downgrading the flag after that point would not
// reverse the SW-upload codec context anyway). Calls with value=false
// are a no-op (debug-logged) once the flag has been latched true; the
// flag never transitions true→false.
//
// IN-FLIGHT FRAME DRAIN: ResetHard alone does not drain upstream-queued
// frames. If the rtmp era of the same Output is still holding decoded
// mediacodec frames in the queues between InputFilter / InputFixer /
// TranscoderNode at the moment SetRawFrameSource(true) is called, those
// frames would reach the freshly-reopened yuv420p encoder. Two outcomes
// were observed against the pinned FFmpeg's `ff_hwcontext_type_mediacodec`
// (which exposes only device-level callbacks — no frames_init /
// frames_get_buffer / transfer_data hooks for the hw_frames_ctx pool):
//
//	(a) ENOSYS from av_hwframe_ctx_init(hw=mediacodec, sw=yuv420p),
//	    causing the encoder to error and the Output to evict —
//	    triggering the recommitDemotedInputToSibling recreate path
//	    in stream_mux_node.go (which then opens a fresh Output[N]
//	    with pix_fmt=yuv420p from the start). This was the originally-
//	    documented behaviour leading to the no-sibling recreate
//	    contract.
//
//	(b) Silent absorption: the MediaCodec Surface buffer pool can
//	    retain in-flux frames during the pix_fmt transition rather
//	    than dispatching them synchronously through hwframe_init,
//	    so the first stale frame is consumed without producing a
//	    packet AND without erroring. Without an error, no eviction
//	    fires, the recreate path never runs, and the encoder stays
//	    in a silent-stall: video received counter grows with camera
//	    frames but processed stays at the pre-tap value.
//
// To eliminate both outcomes Option A drains the upstream feeder
// chain (InputFilter / InputFixer.AutoHeaders / InputFixer.MapStream
// Indices / TranscoderNode) synchronously, AFTER closing the live
// encoder via Encoder.ResetHard. The drain is non-blocking (each
// Processor.Reset discards buffered InputCh / preOutputCh / OutputCh
// items in one pass) — the goal is not to win a race against an
// upstream that is still producing, but to guarantee that the
// specific frames already-queued AT the SetRawFrameSource call site
// are gone before this function returns. Frames produced AFTER
// SetRawFrameSource returns either:
//
//   - originate from the new raw-frame upstream (camera) and arrive
//     as software rawvideo using the demuxer-reported format, which the
//     freshly-reopened pix_fmt=yuv420p encoder accepts after conversion
//     when needed, OR
//   - originate from the still-active rtmp era and hit the new
//     encoder one frame at a time, where outcome (a) above re-applies
//     and the eviction-recovery recreate path covers them.
//
// The CAVEAT framing — "expect one transient eviction" — is now
// historic: with the in-flight drain active, the most common path
// transitions cleanly without an eviction at all. The recreate path
// remains as belt-and-suspenders for permanent-fault edges (codec
// removed, surface gone, etc.).
//
// FFmpeg-version observation: this comment describes behaviour
// against the FFmpeg 7.x source tree pinned in this repo. If a
// future FFmpeg version registers any of those frames_* callbacks
// for the mediacodec hwcontext type, the silent-absorb / ENOSYS
// dichotomy may dissolve. Verify by re-reading
// `ff_hwcontext_type_mediacodec` in libavutil/hwcontext_mediacodec.c
// and re-running the camera-after-rtmp witness against the new
// FFmpeg.
func (s *StreamMux[C]) SetRawFrameSource(ctx context.Context, value bool) {
	if !value {
		// Sticky-true contract: once the flag has been latched true we
		// must not flip it back. The early return ALSO guards against
		// running the per-Output Range mutation loop on a false-value
		// call, which would (a) be wasted work and (b) be misleading in
		// trace logs since no factory mutation is intended.
		if s.RawFrameSource.Load() {
			logger.Debugf(ctx,
				"SetRawFrameSource(false) ignored: flag is already latched true (sticky-true contract)")
		}
		return
	}
	if s.RawFrameSource.Set() {
		// already true; nothing to retro-apply
		return
	}
	s.Outputs.Range(func(_ OutputID, output *Output[C]) bool {
		output.RawFrameSource.Set()
		mutated := applyRawFrameSourceMediaCodecPixFmtToFactory(
			ctx,
			output.TranscoderNode.Processor.Kernel.EncoderFactory,
			true,
		)
		// CRITICAL: f.VideoOptions is the source NewEncoder reads at first
		// frame, but newCodec.Clone deep-copies the Dictionary into the
		// running encoder's InitParams.CustomOptions (codec_params.go:35).
		// So mutating f.VideoOptions in place affects only encoders that
		// have NOT been opened yet. For an encoder that was already opened
		// during the rtmp-only Start phase (the production trigger), we
		// must close it so the kernel re-NewEncoder's it on the next frame
		// against the now-updated f.VideoOptions. ResetHard does that
		// — it closes every running encoder and Reset()s the factory's
		// VideoEncoders slice. The next SendInput triggers fresh
		// NewEncoder, which now sees pix_fmt=yuv420p in f.VideoOptions and
		// opens the codec context on the SW-upload path.
		//
		// The previous reentrant-lock deadlock in
		// kernel.Encoder.resetHard (close-via-LockDo re-acquired the
		// same encoder locker) was fixed by routing the close through
		// the locked variant supplied by LockDo, so this call is now
		// safe.
		if !mutated {
			return true
		}
		if err := output.TranscoderNode.Processor.Kernel.Encoder.ResetHard(ctx); err != nil {
			logger.Errorf(ctx,
				"unable to ResetHard encoder for output %s after late pix_fmt injection: %v",
				output, err,
			)
		}
		// Drain in-flight rtmp-era frames AFTER closing the live encoder.
		// The drain order is upstream → downstream so each stage is
		// empty before the next one is checked: any frame that was
		// holding back-pressure on a downstream stage is now released
		// and flushed by the downstream stage's own drain. See the
		// "IN-FLIGHT FRAME DRAIN" section of SetRawFrameSource's godoc
		// for the full rationale.
		drainUpstreamFeedersForOutput(ctx, output)
		return true
	})
}

// drainUpstreamFeedersForOutput non-blockingly drains the buffered
// packet/frame queues of every Processor in the Output's upstream
// feeder chain (InputFilter Barrier → InputFixer.AutoHeaders →
// InputFixer.MapStreamIndices → TranscoderNode). Each Processor's
// Reset(ctx) call discards any items currently buffered in its
// InputCh / preOutputCh / OutputCh and forwards Reset to the wrapped
// kernel if the kernel implements kerneltypes.Resetter (no-op for
// Barrier / MapStreamIndices / Transcoder; AutoHeaders does drop its
// per-stream detection state so the next post-drain frame triggers a
// fresh header observation).
//
// Why upstream-to-downstream order: Processor channels are bounded
// (DefaultOptionsTranscoder configures InputCh=60 by default — see
// processor/transcoder.go), so a frame stuck on a downstream stage
// can back-pressure the immediately-upstream stage. Draining the
// upstream stage first releases that back-pressure (its sender
// goroutine wakes up and either pushes one more item or sees the
// stage drained); the next stage's drain then sees the result.
// Walking in the reverse order would still produce a one-pass drain
// that LEAVES the upstream stage holding any items it had been
// blocked from forwarding — re-armed back-pressure on the next
// frame would surface as a delayed ENOSYS / silent-consume on the
// freshly-reopened encoder.
//
// The drain is non-blocking by construction: it does not wait for
// upstream sender goroutines to quiesce. Frames that arrive AFTER
// the drain returns are by definition produced post-tap and are
// outside the drain's responsibility (see SetRawFrameSource godoc).
//
// Tolerant of nil sub-nodes (intermediate AutoHeaders is conditional
// on the Output's senderKey audio/video copy semantics — see
// autofix.AutoFixer's NewWithCustomData).
func drainUpstreamFeedersForOutput[C any](
	ctx context.Context,
	output *Output[C],
) {
	logger.Debugf(ctx, "drainUpstreamFeedersForOutput[%s]", output)
	defer logger.Debugf(ctx, "/drainUpstreamFeedersForOutput[%s]", output)

	// Collect the feeder Processors in upstream-to-downstream order.
	// Each entry is a (name, resetter) tuple so failures cite the
	// stage that misbehaved without a reflective Stringer dance.
	type stage struct {
		name     string
		resetter resetter
	}
	stages := []stage{
		{"InputFilter", output.InputFilter.Processor},
	}
	if output.InputFixer != nil {
		if output.InputFixer.AutoHeadersNode != nil {
			stages = append(stages,
				stage{"InputFixer.AutoHeaders", output.InputFixer.AutoHeadersNode.Processor})
		}
		if output.InputFixer.MapStreamIndicesNode != nil {
			stages = append(stages,
				stage{"InputFixer.MapStreamIndices", output.InputFixer.MapStreamIndicesNode.Processor})
		}
	}
	stages = append(stages,
		stage{"TranscoderNode", output.TranscoderNode.Processor})

	for _, s := range stages {
		if s.resetter == nil {
			continue
		}
		if err := s.resetter.Reset(ctx); err != nil {
			// A drain failure does NOT abort the SetRawFrameSource flow:
			// the encoder has already been ResetHard'd above, so the
			// pix_fmt transition is committed regardless of whether one
			// stage's Reset surfaced an error. We log at Warn (not Error)
			// because the dominant failure mode here is "kernel forwards
			// Reset to a Resetter handler that returned a transient error"
			// — the queue drain itself is non-failing (drainCh in
			// from_kernel.go is just a buffered-channel select).
			logger.Warnf(ctx,
				"drainUpstreamFeedersForOutput[%s]: stage %s Reset returned %v (continuing)",
				output, s.name, err)
		}
	}
}

// resetter is the local-package type alias for any Processor exposing
// Reset(ctx) — i.e. *processor.FromKernel[T] for any kernel T (it
// implements kerneltypes.Resetter unconditionally; see
// processor/from_kernel.go:418). Defining it locally avoids importing
// the kernel/types package from a streammux preset file purely to name
// the existing interface.
type resetter interface {
	Reset(ctx context.Context) error
}

// forceRawFrameSourceMediaCodecPixFmt appends pix_fmt=yuv420p to videoOptions
// when rawFrameSource is true, the encoder is a MediaCodec encoder, and no
// explicit pix_fmt is already set. It is a no-op otherwise. Returns the
// (possibly extended) options slice.
func forceRawFrameSourceMediaCodecPixFmt(
	ctx context.Context,
	videoOptions globaltypes.DictionaryItems,
	rawFrameSource bool,
	hwDeviceType types.HardwareDeviceType,
	codecName codec.Name,
) globaltypes.DictionaryItems {
	if !rawFrameSource {
		return videoOptions
	}
	if !isMediaCodecEncoderRequest(hwDeviceType, codecName) {
		return videoOptions
	}
	if videoOptions.GetFirst("pix_fmt") != nil {
		// caller already set an explicit pix_fmt; do not override
		return videoOptions
	}
	logger.Infof(ctx,
		"raw-frame source + MediaCodec encoder: forcing pix_fmt=%s to disable surface-passthrough trap",
		rawFrameSourceMediaCodecPixFmt,
	)
	return append(videoOptions, globaltypes.DictionaryItem{
		Key:   "pix_fmt",
		Value: rawFrameSourceMediaCodecPixFmt,
	})
}

func isMediaCodecEncoderRequest(
	hwDeviceType types.HardwareDeviceType,
	codecName codec.Name,
) bool {
	switch {
	case hwDeviceType == globaltypes.HardwareDeviceTypeMediaCodec:
		return true
	case strings.HasSuffix(string(codecName), "_mediacodec"):
		return true
	default:
		return false
	}
}

// applyRawFrameSourceMediaCodecPixFmtToFactory injects
// pix_fmt=rawFrameSourceMediaCodecPixFmt into the encoder factory when
// the (rawFrameSource && MediaCodec && no explicit pix_fmt) precondition
// holds. Used by retroactive callers (StreamMux when RawFrameSource
// flips on AFTER the initial reconfigureEncoder ran, e.g. wingout's
// hot-add of camera inputs after Start) to arm the fix on encoder
// factories that were configured before the upstream resource set was
// complete.
//
// Delegates the actual Dictionary write to
// (*codec.NaiveEncoderFactory).SetVideoOption so the cross-package
// boundary is encapsulated by the factory and the streammux helper does
// not reach into VideoOptions directly.
//
// Caller must NOT hold encoderFactory.Locker — SetVideoOption takes it.
// Returns true if a value was written.
func applyRawFrameSourceMediaCodecPixFmtToFactory(
	ctx context.Context,
	encoderFactory *codec.NaiveEncoderFactory,
	rawFrameSource bool,
) bool {
	if !rawFrameSource {
		return false
	}
	if encoderFactory == nil {
		return false
	}
	if !isMediaCodecEncoderRequest(encoderFactory.HardwareDeviceType, encoderFactory.VideoCodec) {
		return false
	}
	wrote, err := encoderFactory.SetVideoOptionIfAbsent(ctx, "pix_fmt", rawFrameSourceMediaCodecPixFmt)
	if err != nil {
		logger.Errorf(ctx, "unable to set pix_fmt=%s on encoder factory: %v",
			rawFrameSourceMediaCodecPixFmt, err)
		return false
	}
	if !wrote {
		// caller already pinned an explicit pix_fmt; do not override
		return false
	}
	logger.Infof(ctx,
		"raw-frame source + MediaCodec encoder: late-injected pix_fmt=%s into live encoder factory to disable surface-passthrough trap",
		rawFrameSourceMediaCodecPixFmt,
	)
	return true
}
