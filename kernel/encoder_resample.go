// encoder_resample.go isolates the audio resampler lifecycle and PCM
// format negotiation from the core encoder plumbing in encoder.go.
//
// All exported behaviour is on streamEncoderLocked / streamEncoder
// receivers — they remain defined in encoder.go alongside the encoder
// kernel itself; only the resampler-specific helpers live here:
//
//   getResampledFrames  — drives one Send + drain through resampler.
//   prepareResampler    — reuse-or-rebuild the resampler on PCM
//                         format changes (input or output).
//   reinitEncoderForResamplerRebuild
//                       — refresh AAC ASC / extradata when the resampler
//                         was actually rebuilt (Bug 6.2).
//   getPCMAudioFormatFromFrame
//                       — adapter from astiav.Frame to codec.PCMAudioFormat.
//
// Tests covering this state machine sit in package kernel
// (encoder_resampler_aliasing_test.go, encoder_resampler_input_change_test.go,
// encoder_resampler_reinit_test.go); moving the implementation here
// does not alter visibility — the test files are already in the same
// package or in kernel_test.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/resampler"
)

func getPCMAudioFormatFromFrame(
	_ context.Context,
	frame *astiav.Frame,
) *codec.PCMAudioFormat {
	if frame == nil {
		return nil
	}
	return &codec.PCMAudioFormat{
		SampleFormat:  frame.SampleFormat(),
		SampleRate:    frame.SampleRate(),
		ChannelLayout: frame.ChannelLayout(),
		ChunkSize:     frame.NbSamples(),
	}
}

func (e *streamEncoderLocked) getResampledFrames(
	ctx context.Context,
	inputFrame *astiav.Frame,
	outPCMFmt codec.PCMAudioFormat,
) (resampledFrames []*astiav.Frame, _err error) {
	// making channels ordered, otherwise resampler may fail
	switch inputFrame.ChannelLayout().Channels() {
	case 1:
		inputFrame.SetChannelLayout(astiav.ChannelLayoutMono)
	case 2:
		inputFrame.SetChannelLayout(astiav.ChannelLayoutStereo)
	}
	inPCMFmt := getPCMAudioFormatFromFrame(ctx, inputFrame)
	logger.Tracef(ctx, "getResampledFrames: in:%v; out:%v", inPCMFmt, outPCMFmt)
	defer func() {
		logger.Tracef(ctx, "/getResampledFrames: in:%v; out:%v: %v %v", inPCMFmt, outPCMFmt, resampledFrames, _err)
	}()

	err := e.prepareResampler(ctx, *inPCMFmt, outPCMFmt)
	if err != nil {
		return nil, fmt.Errorf("unable to prepare the resampler: %w", err)
	}

	logger.Tracef(ctx, "sending the frame (%d, %s, %s) to the resampler; samples: %d", inputFrame.SampleRate(), inputFrame.SampleFormat(), inputFrame.ChannelLayout(), inputFrame.NbSamples())
	err = e.Resampler.SendFrame(ctx, inputFrame)
	if err != nil {
		return nil, fmt.Errorf("unable to send the frame: %w", err)
	}

	for idx := 0; ; idx++ {
		for idx >= len(e.ResampledFrames) {
			newFrame, err := e.Resampler.AllocateOutputFrame(ctx)
			if err != nil {
				return nil, fmt.Errorf("unable to allocate a new output frame: %w", err)
			}
			e.ResampledFrames = append(e.ResampledFrames, newFrame)
		}

		outputFrame := e.ResampledFrames[idx]
		outputFrame.SetSampleRate(outPCMFmt.SampleRate)
		outputFrame.SetChannelLayout(outPCMFmt.ChannelLayout)
		outputFrame.SetSampleFormat(outPCMFmt.SampleFormat)
		logger.Tracef(ctx, "receiving the %dth resampled frame (%d, %s, %s)", idx, outputFrame.SampleRate(), outputFrame.SampleFormat(), outputFrame.ChannelLayout())
		err := e.Resampler.ReceiveFrame(ctx, outputFrame)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			logger.Tracef(ctx, "resampler.ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			if isEOF || isEAgain {
				break
			}
			return nil, fmt.Errorf("unable to receive the frame from the resampler: %w", err)
		}
		logger.Tracef(ctx, "got the %dth resampled frame: samples: %d", idx, outputFrame.NbSamples())
		resampledFrames = append(resampledFrames, outputFrame)
	}

	return resampledFrames, nil
}

func (e *streamEncoderLocked) prepareResampler(
	ctx context.Context,
	inPCMFmt codec.PCMAudioFormat,
	outPCMFmt codec.PCMAudioFormat,
) (_err error) {
	logger.Tracef(ctx, "prepareResampler: in:%v; out:%v", inPCMFmt, outPCMFmt)
	defer func() { logger.Tracef(ctx, "/prepareResampler: in:%v; out:%v: %v", inPCMFmt, outPCMFmt, _err) }()

	if e.Resampler != nil {
		// Reuse only when BOTH the output format and the previously-bound
		// input format match. Comparing only the output format silently
		// reused the resampler across input transitions (e.g. mediamtx
		// upstream interrupt+reconnect causing a momentary cascade-internal
		// channel-layout / sample-rate change), after which
		// resampler.SendFrame returned astiav.ErrInputChanged on every
		// frame and the audio stream stalled at sample_rate=0/channels=0
		// downstream while video recovered normally.
		//
		// FormatInput is nil before the resampler has seen its first
		// frame; in that case the input check is irrelevant and we fall
		// through to the reuse path.
		inputMatches := e.Resampler.FormatInput == nil ||
			e.Resampler.FormatInput.Equal(inPCMFmt)
		if outPCMFmt.Equal(e.Resampler.FormatOutput) && inputMatches {
			logger.Tracef(ctx, "reusing the resampler")
			return nil
		}
		logger.Debugf(ctx, "reinitializing the resampler: %v->(%v->%v)", inPCMFmt, e.Resampler.FormatOutput, outPCMFmt)
		if err := e.Resampler.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close the resampler: %v", err)
		}
		// Reset the resampled-PTS anchor so the rebuilt resampler
		// re-derives output timestamps from the next input frame's PTS
		// rather than continuing the pre-interrupt counter (which would
		// stamp post-reconnect samples at a stale offset).
		e.audioResampleHaveOutAnchor = false
		e.audioResampleNextOutPTS = 0
		// Bug 6.2: a resampler-only rebuild leaves the AAC encoder's
		// AudioSpecificConfig (extradata) stale — it was generated at
		// avcodec_open2() time from the encoder's CodecContext and never
		// regenerated afterwards. Tear down and reopen the encoder so the
		// next ToCodecParameters() emits a fresh ASC, and reset
		// LastInitTS so the codec-params re-publish branch
		// (enableStreamCodecParametersUpdates) detects the advance and
		// pushes the refreshed parameters onto the output stream that
		// downstream consumers read.
		//
		// Encoders with no Reinit capability (Copy / Raw, or platform
		// stubs) are skipped — they have no codec context to reopen.
		e.reinitEncoderForResamplerRebuild(ctx)
	}

	s, err := resampler.New(
		ctx,
		outPCMFmt,
	)
	if err != nil {
		return fmt.Errorf("unable to create a resampler: %w", err)
	}

	e.Resampler = s
	// Return previously-allocated resampled frames to the pool. Dropping
	// them to GC is a slab-aliasing UAF: each frame carries a pool finalizer
	// (pool/pool.go:35) that asynchronously runs av_frame_free; a different
	// pooled Go *Frame may later wrap the same C slab and crash on Unref
	// (prod stack: resampler.New -> Pool.Put -> Frame.Unref, addr=0xbb80).
	for _, f := range e.ResampledFrames {
		frame.Pool.Put(f)
	}
	e.ResampledFrames = nil
	return nil
}

// reinitEncoderForResamplerRebuild closes and reopens the underlying
// audio encoder when the resampler is rebuilt due to a real input/output
// PCM format change. This forces the AAC ASC / extradata to be
// regenerated by avcodec_open2() and resets LastInitTS so the
// codec-params re-publish path (republishCodecParamsIfStale, called
// from the drain callback in encoder.go) detects the advance on the
// next packet and pushes refreshed CodecParameters onto the output
// stream.
//
// The streamEncoder lock is already held by the caller (prepareResampler
// runs inside the encoder LockDo callback at encoder.go), so the
// EncoderFullLocked.Reinit variant — which assumes the lock is held — is
// what e.Encoder.(EncoderReiniter).Reinit dispatches to.
func (e *streamEncoderLocked) reinitEncoderForResamplerRebuild(
	ctx context.Context,
) {
	if e.streamEncoder == nil || e.Encoder == nil {
		return
	}
	reiniter, ok := e.Encoder.(codec.EncoderReiniter)
	if !ok {
		logger.Tracef(ctx, "encoder %T does not implement EncoderReiniter; skipping reinit on resampler rebuild", e.Encoder)
		return
	}
	logger.Debugf(ctx, "reinitializing the audio encoder to refresh ASC/extradata after resampler rebuild")
	if err := reiniter.Reinit(ctx); err != nil {
		logger.Errorf(ctx, "unable to reinit encoder on resampler rebuild: %v", err)
		// Reset LastInitTS even on reinit failure so a subsequent
		// successful encoder reinit (or the next codec-params re-publish)
		// is not silently suppressed by a stale comparison.
	}
	e.streamEncoder.LastInitTS = time.Time{}
}
