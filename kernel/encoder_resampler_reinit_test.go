// encoder_resampler_reinit_test.go covers the encoder-reinit + LastInitTS
// reset that prepareResampler must trigger whenever it rebuilds the
// resampler due to a *real* format change (input or output).
//
// Bug 6.2: after a mediamtx upstream interrupt+reconnect that briefly
// perturbs the cascade-internal audio PCM format, prepareResampler tears
// down and recreates the resampler (commit 0a48112). However, the AAC
// encoder retains the AudioSpecificConfig (ASC / extradata) it generated
// at first open(); the codec-params re-publish branch
// (encoder.go:enableStreamCodecParametersUpdates) only fires when
// LastInitTS advances, which happens exclusively on a full encoder
// reinit — not on a resampler-only rebuild. Downstream consumers
// therefore continue to read a stale ASC and reject the audio stream.
//
// Fix-direction: when prepareResampler rebuilds, also tear down and
// reopen the audio encoder (forcing fresh extradata) and reset
// LastInitTS so the codec-params re-publish path fires on the next
// packet.
//
// Determinism: in-process state transitions only; the fake encoder
// records Reinit calls without invoking real libav.
package kernel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

// reinitRecordingEncoder embeds codec.EncoderRaw to satisfy the full
// codec.Encoder interface with no-op semantics, and adds a Reinit
// implementation that bumps a counter so tests can assert it was called.
type reinitRecordingEncoder struct {
	codec.EncoderRaw
	calls atomic.Int64
}

var _ codec.Encoder = (*reinitRecordingEncoder)(nil)
var _ codec.EncoderReiniter = (*reinitRecordingEncoder)(nil)

func (e *reinitRecordingEncoder) Reinit(_ context.Context) error {
	e.calls.Add(1)
	return nil
}

// MediaType must return audio for the encoder-reinit decision; the
// EncoderRaw default panics.
func (e *reinitRecordingEncoder) MediaType(context.Context) astiav.MediaType {
	return astiav.MediaTypeAudio
}

// newStreamEncoderForReinitTest mirrors newStreamEncoderForResamplerTest
// but additionally wires a recording encoder so prepareResampler's
// reinit-on-rebuild path is observable.
func newStreamEncoderForReinitTest() (*streamEncoderLocked, *reinitRecordingEncoder) {
	enc := &reinitRecordingEncoder{}
	se := &streamEncoder{
		Encoder:                    enc,
		audioResampleHaveOutAnchor: true,
		audioResampleNextOutPTS:    12345,
	}
	// Seed LastInitTS with a fixed non-zero instant so the
	// reset-on-rebuild assertion can detect a transition from non-zero
	// to zero deterministically (no reliance on time.Now).
	se.LastInitTS = time.Unix(1700000000, 0)
	return &streamEncoderLocked{Encoder: enc, streamEncoder: se}, enc
}

// TestPrepareResampler_ReinitsEncoder_OnInputChange is the failing test
// for Bug 6.2: when prepareResampler rebuilds the resampler due to an
// input format change, it must also call Reinit on the audio encoder so
// the AAC ASC / extradata is regenerated and consumers see consistent
// codec parameters.
func TestPrepareResampler_ReinitsEncoder_OnInputChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}
	inA := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    44100,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     1024,
	}
	inB := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     1024,
	}

	e, enc := newStreamEncoderForReinitTest()

	// First call: creates the resampler. No rebuild → no encoder reinit.
	require.NoError(t, e.prepareResampler(ctx, inA, out))
	require.Equal(t, int64(0), enc.calls.Load(),
		"first prepareResampler must not Reinit the encoder")
	require.False(t, e.streamEncoder.LastInitTS.IsZero(),
		"first prepareResampler must not reset LastInitTS")

	// Drive the resampler so FormatInput is bound, then trigger an
	// input-format change.
	frameA := buildAudioFrame(t, inA)
	defer func() {
		// Frame is owned by the resampler now; do nothing.
		_ = frameA
	}()
	require.NoError(t, e.Resampler.SendFrame(ctx, frameA))
	require.NotNil(t, e.Resampler.FormatInput)

	// Second call with different input format: rebuilds resampler →
	// MUST also reinit the encoder and reset LastInitTS.
	require.NoError(t, e.prepareResampler(ctx, inB, out))
	require.Equal(t, int64(1), enc.calls.Load(),
		"prepareResampler rebuild must Reinit the audio encoder so ASC is regenerated")
	require.True(t, e.streamEncoder.LastInitTS.IsZero(),
		"prepareResampler rebuild must reset LastInitTS so codec params re-publish fires")
}

// TestPrepareResampler_ReinitsEncoder_OnOutputChange covers the same
// invariant on the output-format-change branch (which already triggered
// resampler rebuild before Bug 6.2 was filed).
func TestPrepareResampler_ReinitsEncoder_OnOutputChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    44100,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     1024,
	}
	outA := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}
	outB := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     1024,
	}

	e, enc := newStreamEncoderForReinitTest()

	require.NoError(t, e.prepareResampler(ctx, in, outA))
	require.Equal(t, int64(0), enc.calls.Load())

	require.NoError(t, e.prepareResampler(ctx, in, outB))
	require.Equal(t, int64(1), enc.calls.Load(),
		"output-format-change rebuild must also Reinit the encoder")
}

// TestPrepareResampler_DoesNotReinit_OnReuse verifies the no-rebuild
// fast path: identical input+output formats must NOT call Reinit (which
// would churn the encoder and lose buffered frames for no reason).
func TestPrepareResampler_DoesNotReinit_OnReuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}
	in := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    44100,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     1024,
	}

	e, enc := newStreamEncoderForReinitTest()

	require.NoError(t, e.prepareResampler(ctx, in, out))
	frameA := buildAudioFrame(t, in)
	require.NoError(t, e.Resampler.SendFrame(ctx, frameA))
	require.NoError(t, e.prepareResampler(ctx, in, out))
	require.Equal(t, int64(0), enc.calls.Load(),
		"resampler-reuse path must not Reinit the encoder")
}
