package stream

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStreamPair(t *testing.T) (*astiav.Stream, *astiav.Stream) {
	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)

	encoder := astiav.FindEncoderByName("libx264")
	if encoder == nil {
		encoder = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoder)

	src := fmtCtx.NewStream(encoder)
	require.NotNil(t, src)
	dst := fmtCtx.NewStream(encoder)
	require.NotNil(t, dst)

	return src, dst
}

func TestCopyNonCodecParameters(t *testing.T) {
	src, dst := newStreamPair(t)

	src.SetTimeBase(astiav.NewRational(1, 90000))
	src.SetAvgFrameRate(astiav.NewRational(30, 1))
	src.SetRFrameRate(astiav.NewRational(30, 1))
	src.SetStartTime(12345)

	CopyNonCodecParameters(dst, src)

	assert.Equal(t, src.TimeBase().Num(), dst.TimeBase().Num())
	assert.Equal(t, src.TimeBase().Den(), dst.TimeBase().Den())
	assert.Equal(t, src.AvgFrameRate().Num(), dst.AvgFrameRate().Num())
	assert.Equal(t, src.AvgFrameRate().Den(), dst.AvgFrameRate().Den())
	assert.Equal(t, src.StartTime(), dst.StartTime())
}

func TestCopyParameters(t *testing.T) {
	src, dst := newStreamPair(t)

	src.CodecParameters().SetCodecID(astiav.CodecIDH264)
	src.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	src.SetTimeBase(astiav.NewRational(1, 90000))

	err := CopyParameters(context.Background(), dst, src)
	require.NoError(t, err)

	assert.Equal(t, astiav.CodecIDH264, dst.CodecParameters().CodecID())
	assert.Equal(t, astiav.MediaTypeVideo, dst.CodecParameters().MediaType())
	assert.Equal(t, src.TimeBase().Num(), dst.TimeBase().Num())
}

func TestCopySideData(t *testing.T) {
	src, dst := newStreamPair(t)

	// Just exercise the function with streams that have no display matrix
	CopySideData(dst, src)
	// No panic means success for the no-display-matrix case
}

func TestCopyParameters_Independence(t *testing.T) {
	src, dst := newStreamPair(t)

	src.CodecParameters().SetCodecID(astiav.CodecIDH264)
	src.SetTimeBase(astiav.NewRational(1, 30))

	err := CopyParameters(context.Background(), dst, src)
	require.NoError(t, err)

	// Modify src after copy, dst should be independent
	src.CodecParameters().SetCodecID(astiav.CodecIDAac)
	src.SetTimeBase(astiav.NewRational(1, 44100))

	assert.Equal(t, astiav.CodecIDH264, dst.CodecParameters().CodecID())
	assert.Equal(t, 30, dst.TimeBase().Den())
}
