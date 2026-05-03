// validate_test.go covers PCMAudioFormat validation in resampler.New.

package resampler

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

func TestValidateOutputFormat_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}))
}

func TestValidateOutputFormat_ZeroSampleFormat(t *testing.T) {
	t.Parallel()
	err := validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatNone,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "SampleFormat")
}

func TestValidateOutputFormat_ZeroSampleRate(t *testing.T) {
	t.Parallel()
	err := validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    0,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "SampleRate")
}

func TestValidateOutputFormat_ZeroChannelLayout(t *testing.T) {
	t.Parallel()
	err := validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayout{},
		ChunkSize:     1024,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ChannelLayout")
}

func TestValidateOutputFormat_ZeroChunkSize(t *testing.T) {
	t.Parallel()
	err := validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ChunkSize")
}

func TestValidateOutputFormat_AccumulatesAllErrors(t *testing.T) {
	t.Parallel()
	err := validateOutputFormat(codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatNone,
		SampleRate:    0,
		ChannelLayout: astiav.ChannelLayout{},
		ChunkSize:     0,
	})
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "SampleFormat")
	require.Contains(t, msg, "SampleRate")
	require.Contains(t, msg, "ChannelLayout")
	require.Contains(t, msg, "ChunkSize")
}

func TestNew_ZeroChunkSize_ReturnsDescriptiveError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, err := New(ctx, codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     0,
	})
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "ChunkSize")
	// Must NOT be the opaque libav message anymore.
	require.NotContains(t, err.Error(), "Invalid argument")
}

func TestNew_ZeroChannelLayout_ReturnsDescriptiveError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, err := New(ctx, codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayout{},
		ChunkSize:     1024,
	})
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "ChannelLayout")
	require.NotContains(t, err.Error(), "Invalid argument")
}

func TestNew_NoneSampleFormat_ReturnsDescriptiveError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, err := New(ctx, codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatNone,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	})
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "SampleFormat")
	require.NotContains(t, err.Error(), "Invalid argument")
}

func TestNew_ValidFormat_StillWorks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, err := New(ctx, codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })
}
