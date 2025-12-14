package resampler

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
)

func defaultPCMFormat() codec.PCMAudioFormat {
	return codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFlt,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     8,
	}
}

func TestGetPCMFormatFromFrame(t *testing.T) {
	t.Run("NilFrame", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, getPCMFormatFromFrame(context.Background(), nil))
	})

	t.Run("PopulatedFrame", func(t *testing.T) {
		t.Parallel()

		fr := frame.Pool.Get()
		defer frame.Pool.Put(fr)
		fr.Unref()
		fr.SetSampleFormat(astiav.SampleFormatS16)
		fr.SetSampleRate(44100)
		fr.SetChannelLayout(astiav.ChannelLayoutMono)
		fr.SetNbSamples(123)

		got := getPCMFormatFromFrame(context.Background(), fr)
		require.NotNil(t, got)

		expected := codec.PCMAudioFormat{
			SampleFormat:  astiav.SampleFormatS16,
			SampleRate:    44100,
			ChannelLayout: astiav.ChannelLayoutMono,
			ChunkSize:     123,
		}
		require.True(t, expected.Equal(*got))
	})
}

func TestResamplerAllocateOutputFrame(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fmt := defaultPCMFormat()
	r, err := New(ctx, fmt)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })

	out, err := r.AllocateOutputFrame(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { frame.Pool.Put(out) })

	require.Equal(t, fmt.ChunkSize, out.NbSamples())
	require.Equal(t, fmt.ChannelLayout, out.ChannelLayout())
	require.Equal(t, fmt.SampleFormat, out.SampleFormat())
	require.Equal(t, fmt.SampleRate, out.SampleRate())
}

func TestResamplerReceiveFrameFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fmt := defaultPCMFormat()
	r, err := New(ctx, fmt)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })

	out, err := r.AllocateOutputFrame(ctx)
	require.NoError(t, err)
	defer frame.Pool.Put(out)

	require.ErrorIs(t, r.ReceiveFrame(ctx, out), astiav.ErrEof)

	writeSamples(t, r, fmt.ChunkSize/2)
	require.ErrorIs(t, r.ReceiveFrame(ctx, out), astiav.ErrEagain)

	writeSamples(t, r, fmt.ChunkSize)
	require.NoError(t, r.ReceiveFrame(ctx, out))
	require.Equal(t, fmt.ChunkSize, out.NbSamples())

	flushFrame, err := r.AllocateOutputFrame(ctx)
	require.NoError(t, err)
	defer frame.Pool.Put(flushFrame)
	require.NoError(t, r.Flush(ctx, flushFrame))
	require.Equal(t, fmt.ChunkSize/2, flushFrame.NbSamples())

	require.ErrorIs(t, r.ReceiveFrame(ctx, out), astiav.ErrEof)
}

func TestResamplerSendFrameValidations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fmt := defaultPCMFormat()
	r, err := New(ctx, fmt)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })

	firstFormat := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatS16,
		SampleRate:    44100,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     4,
	}

	secondFormat := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatS16,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutMono,
		ChunkSize:     4,
	}

	firstFrame := buildPCMFrame(t, firstFormat)
	defer frame.Pool.Put(firstFrame)

	secondFrame := buildPCMFrame(t, secondFormat)
	defer frame.Pool.Put(secondFrame)

	err = r.SendFrame(ctx, firstFrame)
	require.NoError(t, err)
	require.NotNil(t, r.FormatInput)
	require.True(t, firstFormat.Equal(*r.FormatInput))

	err = r.SendFrame(ctx, secondFrame)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input frame format changed")
}

func TestResamplerUnspecifiedChannelOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fmtOut := defaultPCMFormat()
	r, err := New(ctx, fmtOut)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })

	inputFormat := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatS16,
		SampleRate:    44100,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     fmtOut.ChunkSize,
	}
	inputFormat.ChannelLayout.SetOrder(astiav.ChannelOrderUnspecified)

	firstFrame := buildPCMFrame(t, inputFormat)
	defer frame.Pool.Put(firstFrame)

	secondFrame := buildPCMFrame(t, inputFormat)
	defer frame.Pool.Put(secondFrame)

	require.NoError(t, r.SendFrame(ctx, firstFrame))
	require.NotNil(t, r.FormatInput)

	err = r.SendFrame(ctx, secondFrame)
	require.Error(t, err)
	require.ErrorIs(t, err, astiav.ErrInputChanged)
}

func writeSamples(t *testing.T, r *Resampler, samples int) {
	t.Helper()
	pcmFrame := buildPCMFrame(t, codec.PCMAudioFormat{
		SampleFormat:  r.FormatOutput.SampleFormat,
		SampleRate:    r.FormatOutput.SampleRate,
		ChannelLayout: r.FormatOutput.ChannelLayout,
		ChunkSize:     samples,
	})
	defer func() {
		frame.Pool.Put(pcmFrame)
	}()
	_, err := r.AudioFifo.Write(pcmFrame)
	require.NoError(t, err)
}

func buildPCMFrame(t *testing.T, fmt codec.PCMAudioFormat) *astiav.Frame {
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
