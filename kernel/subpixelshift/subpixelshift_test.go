package subpixelshift

import (
	"context"
	"sync/atomic"
	"testing"

	astiav "github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// makeVideoFrameInput creates a YUV420P frame with the given dimensions and
// returns an InputUnion wrapping it. The cleanup function frees the underlying
// astiav objects.
// Agent-generated test helper.
func makeVideoFrameInput(
	t *testing.T,
	width, height int,
) (packetorframe.InputUnion, func()) {
	t.Helper()

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)

	f := astiav.AllocFrame()
	f.SetWidth(width)
	f.SetHeight(height)
	f.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(t, f.AllocBuffer(0))

	si := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 1000),
	}
	fi := frame.BuildInput(f, 0, si)
	cleanup := func() {
		cp.Free()
		f.Free()
	}
	return packetorframe.InputUnion{Frame: &fi}, cleanup
}

// makeAudioFrameInput creates a minimal audio frame InputUnion for passthrough testing.
// Agent-generated test helper.
func makeAudioFrameInput(t *testing.T) (packetorframe.InputUnion, func()) {
	t.Helper()

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)

	f := astiav.AllocFrame()
	f.SetNbSamples(1024)
	f.SetSampleRate(48000)
	f.SetSampleFormat(astiav.SampleFormatFltp)
	cl := astiav.ChannelLayoutStereo
	f.SetChannelLayout(cl)
	require.NoError(t, f.AllocBuffer(0))

	si := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}
	fi := frame.BuildInput(f, 0, si)
	cleanup := func() {
		cp.Free()
		f.Free()
	}
	return packetorframe.InputUnion{Frame: &fi}, cleanup
}

// Agent-generated test.
func TestKernelNew(t *testing.T) {
	k := New(
		WithScale(3),
		WithBufferSize(16),
		WithMotionMode(MotionModePerBlock),
	)

	assert.Equal(t, int32(3), k.Scale.Load())
	assert.Equal(t, int32(16), k.BufferSize.Load())
	assert.Equal(t, int32(MotionModePerBlock), k.MotionMode.Load())

	// Defaults for unset fields.
	assert.Equal(t, int32(ColorModeAuto), k.ColorMode.Load())
	assert.Equal(t, int32(StartupModePassthrough), k.StartupMode.Load())
	assert.Equal(t, int32(16), k.BlockSize.Load())
}

// Agent-generated test.
func TestKernelString(t *testing.T) {
	k := New(WithScale(4), WithBufferSize(12))
	s := k.String()
	assert.Contains(t, s, "SubPixelShift")
	assert.Contains(t, s, "4")
	assert.Contains(t, s, "12")
}

// Agent-generated test.
func TestKernelRejectsNilInput(t *testing.T) {
	k := New()
	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	err := k.SendInput(ctx, packetorframe.InputUnion{}, outputCh)
	require.Error(t, err)
}

// Agent-generated test.
func TestKernelPassthroughWhenDisabled(t *testing.T) {
	enabled := &atomic.Bool{}
	enabled.Store(false)
	k := New()
	k.Enabled = enabled

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	input, cleanup := makeVideoFrameInput(t, 32, 32)
	defer cleanup()

	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	require.Equal(t, 1, len(outputCh))
	out := <-outputCh
	require.NotNil(t, out.Frame)
}

// Agent-generated test.
func TestKernelBuffersBeforeOutput(t *testing.T) {
	k := New(
		WithStartupMode(StartupModeBuffer),
		WithBufferSize(8),
		WithScale(2),
	)

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	input, cleanup := makeVideoFrameInput(t, 32, 32)
	defer cleanup()

	// Send 1 frame — with StartupModeBuffer, no output yet.
	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)
	assert.Equal(t, 0, len(outputCh))
}

// Agent-generated test.
func TestKernelOutputDimensions(t *testing.T) {
	k := New(
		WithStartupMode(StartupModePassthrough),
		WithScale(2),
		WithBufferSize(8),
	)

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	input, cleanup := makeVideoFrameInput(t, 64, 64)
	defer cleanup()

	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)
	require.Equal(t, 1, len(outputCh))

	out := <-outputCh
	require.NotNil(t, out.Frame)
	assert.Equal(t, 128, out.Frame.Width())
	assert.Equal(t, 128, out.Frame.Height())
}

// Agent-generated test.
func TestKernelClose(t *testing.T) {
	k := New()
	ctx := context.Background()

	select {
	case <-k.CloseChan():
		t.Fatal("CloseChan should not be closed before Close")
	default:
	}

	err := k.Close(ctx)
	require.NoError(t, err)

	select {
	case <-k.CloseChan():
	default:
		t.Fatal("CloseChan should be closed after Close")
	}

	// Double close must not panic.
	err = k.Close(ctx)
	require.NoError(t, err)
}

// Agent-generated test.
func TestKernelGenerate(t *testing.T) {
	k := New()
	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	err := k.Generate(ctx, outputCh)
	require.NoError(t, err)
	assert.Equal(t, 0, len(outputCh))
}

// Agent-generated test.
func TestKernelAudioPassthrough(t *testing.T) {
	k := New(WithScale(2), WithBufferSize(8))
	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 4)

	input, cleanup := makeAudioFrameInput(t)
	defer cleanup()

	err := k.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	require.Equal(t, 1, len(outputCh))
	out := <-outputCh
	require.NotNil(t, out.Frame)
}
