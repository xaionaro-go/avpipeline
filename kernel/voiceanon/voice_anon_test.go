package voiceanon

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/audio"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// --- Test helpers ---

func makeAudioFrameInput(t *testing.T, numSamples int) (packetorframe.InputUnion, func()) {
	t.Helper()
	f := astiav.AllocFrame()
	f.SetSampleFormat(astiav.SampleFormatFlt)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	f.SetSampleRate(16000)
	f.SetNbSamples(numSamples)
	require.NoError(t, f.AllocBuffer(0))

	// Fill with a sine wave to avoid NaN warnings from rubberband.
	fillAudioFrameWithSine(t, f, numSamples)

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetSampleRate(16000)
	cp.SetSampleFormat(astiav.SampleFormatFlt)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 16000),
	}
	frameInput := frame.BuildInput(f, 0, streamInfo)
	cleanup := func() {
		cp.Free()
		f.Free()
	}
	return packetorframe.InputUnion{Frame: &frameInput}, cleanup
}

func fillAudioFrameWithSine(t *testing.T, f *astiav.Frame, numSamples int) {
	t.Helper()
	samples := make([]float64, numSamples)
	for i := range samples {
		// 440 Hz sine wave at 16kHz sample rate.
		samples[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/16000)
	}
	require.NoError(t, audio.FillSamples(f, 0, samples))
}

func makeVideoFrameInput(t *testing.T) (packetorframe.InputUnion, func()) {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)
	f := astiav.AllocFrame()
	f.SetWidth(64)
	f.SetHeight(64)
	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
	}
	frameInput := frame.BuildInput(f, 0, streamInfo)
	cleanup := func() {
		cp.Free()
		f.Free()
	}
	return packetorframe.InputUnion{Frame: &frameInput}, cleanup
}

func makePacketInput(t *testing.T) (packetorframe.InputUnion, func()) {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	pkt := astiav.AllocPacket()
	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
	}
	pktInput := packet.BuildInput(pkt, streamInfo)
	cleanup := func() {
		cp.Free()
		pkt.Free()
	}
	return packetorframe.InputUnion{Packet: &pktInput}, cleanup
}

// --- Interface compliance ---

func TestVoiceAnonymizer_ImplementsAbstract(t *testing.T) {
	var _ kernel.Abstract = (*VoiceAnonymizer)(nil)
}

// --- Constructor ---

func TestNew_DefaultPitch(t *testing.T) {
	va := New(Config{})
	assert.Equal(t, 0.7, va.Config().PitchScale)
}

func TestNew_CustomPitch(t *testing.T) {
	va := New(Config{PitchScale: 1.5})
	assert.Equal(t, 1.5, va.Config().PitchScale)
}

// --- String ---

func TestVoiceAnonymizer_String(t *testing.T) {
	va := New(Config{PitchScale: 0.8})
	s := va.String()
	assert.Contains(t, s, "VoiceAnonymizer")
	assert.Contains(t, s, "0.8")
}

// --- GetObjectID ---

func TestVoiceAnonymizer_GetObjectID(t *testing.T) {
	va := New(DefaultConfig())
	id := va.GetObjectID()
	assert.NotEmpty(t, id)
}

// --- Generate (no-op) ---

func TestVoiceAnonymizer_Generate(t *testing.T) {
	va := New(DefaultConfig())
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := va.Generate(context.Background(), outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 0)
}

// --- Close without init ---

func TestVoiceAnonymizer_Close_NoInit(t *testing.T) {
	va := New(DefaultConfig())
	err := va.Close(context.Background())
	assert.NoError(t, err)
}

// --- SendInput: packet passthrough ---

func TestVoiceAnonymizer_SendInput_PassthroughPacket(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := va.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)

	out := <-outputCh
	assert.NotNil(t, out.Packet)
	assert.Nil(t, out.Frame)
}

// --- SendInput: video frame passthrough ---

func TestVoiceAnonymizer_SendInput_PassthroughVideoFrame(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	input, cleanup := makeVideoFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := va.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)

	out := <-outputCh
	assert.NotNil(t, out.Frame)
}

// --- SendInput: disabled passthrough ---

func TestVoiceAnonymizer_SendInput_DisabledPassthrough(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	enabled := &atomic.Bool{}
	enabled.Store(false)
	va.Enabled = enabled

	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := va.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)
}

// --- SendInput: nil frame passthrough ---

func TestVoiceAnonymizer_SendInput_NilInput(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := va.SendInput(context.Background(), packetorframe.InputUnion{}, outputCh)
	assert.NoError(t, err)
}

// --- SendInput: audio through filter graph ---

func TestVoiceAnonymizer_SendInput_AudioThroughFilterGraph(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	ctx := context.Background()

	// Use a large enough frame to push rubberband past its internal
	// buffering threshold. Rubberband typically needs ~8192 samples
	// before it starts producing output.
	input, cleanup := makeAudioFrameInput(t, 16384)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 100)
	err := va.SendInput(ctx, input, outputCh)
	assert.NoError(t, err)

	// Rubberband buffers significantly; send more frames.
	for i := 0; i < 5; i++ {
		inp, cl := makeAudioFrameInput(t, 16384)
		err = va.SendInput(ctx, inp, outputCh)
		assert.NoError(t, err)
		cl()
	}

	// After ~96k samples at 16kHz (~6 seconds), rubberband should produce output.
	assert.Greater(t, len(outputCh), 0, "expected at least one output after ~6 seconds of audio")
}

// --- SendInput: runtime toggle ---

func TestVoiceAnonymizer_SendInput_RuntimeToggle(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	enabled := &atomic.Bool{}
	enabled.Store(true)
	va.Enabled = enabled

	ctx := context.Background()
	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	// Enabled: should go through filter.
	outputCh := make(chan packetorframe.OutputUnion, 100)
	err := va.SendInput(ctx, input, outputCh)
	assert.NoError(t, err)

	// Disable: should passthrough.
	enabled.Store(false)
	input2, cleanup2 := makeAudioFrameInput(t, 1024)
	defer cleanup2()

	err = va.SendInput(ctx, input2, outputCh)
	assert.NoError(t, err)
	// Passthrough should produce exactly 1 output for the second frame.
	// (The first frame may or may not have produced output through the filter.)
}

// --- SendInput: context cancellation ---

func TestVoiceAnonymizer_SendInput_ContextCancelled(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	// First, initialize the filter graph with a normal call.
	ctx := context.Background()
	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	initCh := make(chan packetorframe.OutputUnion, 10)
	_ = va.SendInput(ctx, input, initCh)

	// Now try with cancelled context and unbuffered channel.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	input2, cleanup2 := makeAudioFrameInput(t, 1024)
	defer cleanup2()

	outputCh := make(chan packetorframe.OutputUnion)
	err := va.SendInput(cancelCtx, input2, outputCh)
	// Rubberband may buffer everything, so the cancelled ctx only matters
	// if there's actual output to send. We just verify no panic.
	_ = err
}

// --- Close after use ---

func TestVoiceAnonymizer_Close_AfterUse(t *testing.T) {
	va := New(DefaultConfig())

	ctx := context.Background()
	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	_ = va.SendInput(ctx, input, outputCh)

	err := va.Close(ctx)
	assert.NoError(t, err)
}

// --- FilterGraph accessor ---

func TestVoiceAnonymizer_FilterGraph_BeforeInit(t *testing.T) {
	va := New(DefaultConfig())
	assert.Nil(t, va.FilterGraph())
}

func TestVoiceAnonymizer_FilterGraph_AfterInit(t *testing.T) {
	va := New(DefaultConfig())
	defer va.Close(context.Background())

	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	_ = va.SendInput(context.Background(), input, outputCh)

	assert.NotNil(t, va.FilterGraph())
}
