// map_stream_indices_test.go tests MapStreamIndices kernel.

package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

type fixedAssigner struct {
	OutputIndex int
}

func (a *fixedAssigner) StreamIndexAssign(
	_ context.Context,
	_ packetorframe.InputUnion,
) ([]int, error) {
	return []int{a.OutputIndex}, nil
}

func TestMapStreamIndices_DoesNotMutateSourceStreamInfo(t *testing.T) {
	ctx := context.Background()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDPcmS16Le)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	sourceStreamInfo := &packetorframetypes.StreamInfo{
		Source:          &Dummy{},
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}

	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 5})
	defer m.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 10)

	// Send first frame.
	f1 := astiav.AllocFrame()
	defer f1.Free()
	f1.SetNbSamples(1024)
	f1.SetSampleRate(48000)
	f1.SetChannelLayout(astiav.ChannelLayoutMono)
	f1.SetSampleFormat(astiav.SampleFormatS16)
	input1 := packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f1, 0, sourceStreamInfo)),
	}
	err := m.SendInput(ctx, input1, outputCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 0, sourceStreamInfo.StreamIndex,
		"source StreamInfo.StreamIndex must not be mutated by MapStreamIndices")

	// Send second frame — should still hit cache (StreamIndex unchanged).
	f2 := astiav.AllocFrame()
	defer f2.Free()
	f2.SetNbSamples(1024)
	f2.SetSampleRate(48000)
	f2.SetChannelLayout(astiav.ChannelLayoutMono)
	f2.SetSampleFormat(astiav.SampleFormatS16)
	input2 := packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f2, 0, sourceStreamInfo)),
	}
	err = m.SendInput(ctx, input2, outputCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 0, sourceStreamInfo.StreamIndex,
		"source StreamInfo.StreamIndex must not be mutated after second frame")

	// Verify both outputs have the mapped stream index.
	testifyassert.Len(t, outputCh, 2)
	out1 := <-outputCh
	out2 := <-outputCh
	testifyassert.Equal(t, 5, out1.GetStreamIndex())
	testifyassert.Equal(t, 5, out2.GetStreamIndex())

	// Verify only one output stream was created (no index leak).
	testifyassert.Len(t, m.outputStreams, 1)
}

// stubDecoderSource is a fmt.Stringer that also implements
// codec.GetDecoderer, so MapStreamIndices.GetDecoder must forward to it.
type stubDecoderSource struct {
	Decoder *codec.Decoder
}

func (s *stubDecoderSource) String() string             { return "stubDecoderSource" }
func (s *stubDecoderSource) GetDecoder() *codec.Decoder { return s.Decoder }

// stubPlainSource is a fmt.Stringer that does NOT implement
// codec.GetDecoderer, so MapStreamIndices.GetDecoder must return nil.
type stubPlainSource struct{}

func (s *stubPlainSource) String() string { return "stubPlainSource" }

func sendOneFrame(
	t *testing.T,
	ctx context.Context,
	m *MapStreamIndices,
	source packetorframe.AbstractSource,
) {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDPcmS16Le)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	si := &packetorframetypes.StreamInfo{
		Source:          source,
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}
	f := astiav.AllocFrame()
	t.Cleanup(f.Free)
	f.SetNbSamples(1024)
	f.SetSampleRate(48000)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	f.SetSampleFormat(astiav.SampleFormatS16)

	outputCh := make(chan packetorframe.OutputUnion, 4)
	err := m.SendInput(ctx, packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f, 0, si)),
	}, outputCh)
	require.NoError(t, err)
}

// TestMapStreamIndices_GetDecoder_NoInputYet asserts GetDecoder returns nil
// when no input has been routed through MapStreamIndices yet (the
// upstream-source field is uninitialised).
func TestMapStreamIndices_GetDecoder_NoInputYet(t *testing.T) {
	ctx := context.Background()
	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 5})
	defer m.Close(ctx)

	testifyassert.Nil(t, m.GetDecoder(),
		"GetDecoder must return nil before any input has been observed")
}

// TestMapStreamIndices_GetDecoder_ForwardsWhenSupported asserts that, after
// at least one input has been routed whose upstream source implements
// codec.GetDecoderer, GetDecoder forwards to that source's decoder.
func TestMapStreamIndices_GetDecoder_ForwardsWhenSupported(t *testing.T) {
	ctx := context.Background()
	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 5})
	defer m.Close(ctx)

	want := &codec.Decoder{}
	src := &stubDecoderSource{Decoder: want}
	sendOneFrame(t, ctx, m, src)

	got := m.GetDecoder()
	testifyassert.Same(t, want, got,
		"GetDecoder must forward to the upstream source's decoder")
}

// TestMapStreamIndices_GetDecoder_NilWhenSourceNotDecoderer asserts that,
// when the upstream source does not implement codec.GetDecoderer,
// GetDecoder returns nil rather than panicking.
func TestMapStreamIndices_GetDecoder_NilWhenSourceNotDecoderer(t *testing.T) {
	ctx := context.Background()
	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 5})
	defer m.Close(ctx)

	sendOneFrame(t, ctx, m, &stubPlainSource{})

	testifyassert.Nil(t, m.GetDecoder(),
		"GetDecoder must return nil when upstream source does not implement codec.GetDecoderer")
}

// TestMapStreamIndices_FramePropagatesUpstreamSource asserts that frames
// emitted by MapStreamIndices keep their upstream source intact (rather
// than being rewritten to `m`). Downstream encoder construction relies on
// that pass-through to type-assert each frame's source to
// codec.GetDecoderer per-stream, avoiding the cross-stream race that the
// single-slot upstreamSource cache would suffer when audio and video
// frames interleave.
func TestMapStreamIndices_FramePropagatesUpstreamSource(t *testing.T) {
	ctx := context.Background()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDPcmS16Le)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	upstream := &stubDecoderSource{Decoder: &codec.Decoder{}}
	si := &packetorframetypes.StreamInfo{
		Source:          upstream,
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}

	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 7})
	defer m.Close(ctx)

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetNbSamples(1024)
	f.SetSampleRate(48000)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	f.SetSampleFormat(astiav.SampleFormatS16)

	outputCh := make(chan packetorframe.OutputUnion, 4)
	err := m.SendInput(ctx, packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f, 0, si)),
	}, outputCh)
	require.NoError(t, err)

	require.Len(t, outputCh, 1)
	out := <-outputCh
	testifyassert.Equal(t, 7, out.GetStreamIndex())

	// The output frame's source must be the upstream source (so encoder
	// construction can type-assert to codec.GetDecoderer per-stream),
	// not MapStreamIndices itself.
	_, fOut := out.Unwrap()
	require.NotNil(t, fOut)
	got := fOut.StreamInfo.Source
	testifyassert.Same(t, upstream, got,
		"output frame source must be the upstream source, not MapStreamIndices")
}

// TestMapStreamIndices_FrameFallsBackToSelfWhenUpstreamNil asserts that
// when an input frame has no upstream source (e.g. synthetic generators),
// MapStreamIndices uses itself as the frame source. This preserves the
// invariant that a frame.Source is always non-nil.
func TestMapStreamIndices_FrameFallsBackToSelfWhenUpstreamNil(t *testing.T) {
	ctx := context.Background()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDPcmS16Le)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	si := &packetorframetypes.StreamInfo{
		Source:          nil, // no upstream advertised
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}

	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 7})
	defer m.Close(ctx)

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetNbSamples(1024)
	f.SetSampleRate(48000)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	f.SetSampleFormat(astiav.SampleFormatS16)

	outputCh := make(chan packetorframe.OutputUnion, 4)
	err := m.SendInput(ctx, packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f, 0, si)),
	}, outputCh)
	require.NoError(t, err)

	require.Len(t, outputCh, 1)
	out := <-outputCh
	_, fOut := out.Unwrap()
	require.NotNil(t, fOut)
	got := fOut.StreamInfo.Source
	testifyassert.Same(t, m, got,
		"output frame source must fall back to MapStreamIndices when upstream is nil")
}
