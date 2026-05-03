// encoder_resampler_input_change_test.go covers the resampler-rebuild
// path when the *input* PCM format changes between calls to
// prepareResampler.
//
// Regression: prior to the fix, prepareResampler only compared the
// *output* PCM format against the existing resampler's FormatOutput.
// On an input format change (e.g. mediamtx upstream interruption +
// reconnect altering the cascade-internal channel layout / sample rate
// momentarily) the existing resampler was reused; resampler.SendFrame
// then rejected every frame with astiav.ErrInputChanged, the encoder
// never produced packets and downstream consumers observed an audio
// stream stuck at sample_rate=0/channels=0/0-decodable-frames despite
// the video stream recovering normally.
//
// Determinism: this test exercises in-process state transitions only;
// no external libav decoder, no goroutine timing.
package kernel
import (
	"context"
	"testing"
	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
)
func newStreamEncoderForResamplerTest() *streamEncoderLocked {
	se := &streamEncoder{
		audioResampleHaveOutAnchor: true,
		audioResampleNextOutPTS:    12345,
	}
	return &streamEncoderLocked{streamEncoder: se}
}
func buildAudioFrame(t *testing.T, fmt codec.PCMAudioFormat) *astiav.Frame {
	t.Helper()
	fr := frame.Pool.Get()
	fr.Unref()
	fr.SetSampleFormat(fmt.SampleFormat)
	fr.SetSampleRate(fmt.SampleRate)
	fr.SetChannelLayout(fmt.ChannelLayout)
	fr.SetNbSamples(fmt.ChunkSize)
	require.NoError(t, fr.AllocBuffer(0))
	return fr
}
// TestPrepareResamplerRebuildsOnInputChange verifies that
// prepareResampler tears down and recreates the resampler whenever
// the input format differs from the previously-bound input format,
// even when the output format is unchanged.
//
// Without the fix, the resampler is reused with FormatInput still
// pinned to the old format; the very next SendFrame returns
// astiav.ErrInputChanged.
func TestPrepareResamplerRebuildsOnInputChange(t *testing.T) {
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
	e := newStreamEncoderForResamplerTest()
	require.NoError(t, e.prepareResampler(ctx, inA, out))
	require.NotNil(t, e.Resampler)
	frameA := buildAudioFrame(t, inA)
	defer frame.Pool.Put(frameA)
	require.NoError(t, e.Resampler.SendFrame(ctx, frameA))
	require.NotNil(t, e.Resampler.FormatInput)
	require.Equal(t, 44100, e.Resampler.FormatInput.SampleRate)
	prevResampler := e.Resampler
	require.NoError(t, e.prepareResampler(ctx, inB, out))
	require.NotSame(t, prevResampler, e.Resampler, "resampler must be rebuilt on input format change")
	require.Nil(t, e.Resampler.FormatInput, "fresh resampler must have nil FormatInput")
	frameB := buildAudioFrame(t, inB)
	defer frame.Pool.Put(frameB)
	require.NoError(t, e.Resampler.SendFrame(ctx, frameB), "first frame after rebuild must succeed")
}
// TestPrepareResamplerReusesOnSameInputAndOutput verifies the fast
// path: identical input and output formats must reuse the existing
// resampler (no churn, no FIFO reset).
func TestPrepareResamplerReusesOnSameInputAndOutput(t *testing.T) {
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
	e := newStreamEncoderForResamplerTest()
	require.NoError(t, e.prepareResampler(ctx, in, out))
	first := e.Resampler
	require.NotNil(t, first)
	frameA := buildAudioFrame(t, in)
	defer frame.Pool.Put(frameA)
	require.NoError(t, first.SendFrame(ctx, frameA))
	require.NoError(t, e.prepareResampler(ctx, in, out))
	require.Same(t, first, e.Resampler, "resampler must be reused when neither input nor output format change")
}
// TestPrepareResamplerRebuildsOnOutputChange retains the pre-existing
// guarantee: an output format change forces a rebuild.
func TestPrepareResamplerRebuildsOnOutputChange(t *testing.T) {
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
	e := newStreamEncoderForResamplerTest()
	require.NoError(t, e.prepareResampler(ctx, in, outA))
	first := e.Resampler
	require.NoError(t, e.prepareResampler(ctx, in, outB))
	require.NotSame(t, first, e.Resampler, "resampler must be rebuilt on output format change")
}
