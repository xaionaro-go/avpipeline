package codec

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

func TestSetupPixelFormat_ExplicitYUV420POverridesParameterNV12(t *testing.T) {
	ctx := context.Background()

	encoder := astiav.FindEncoderByName("libx264")
	if encoder == nil {
		t.Skip("libx264 encoder is not available")
	}

	codecContext := astiav.AllocCodecContext(encoder)
	require.NotNil(t, codecContext)
	defer codecContext.Free()

	codecParameters := astiav.AllocCodecParameters()
	require.NotNil(t, codecParameters)
	defer codecParameters.Free()
	codecParameters.SetMediaType(astiav.MediaTypeVideo)
	codecParameters.SetCodecID(astiav.CodecIDH264)
	codecParameters.SetWidth(1920)
	codecParameters.SetHeight(1080)
	codecParameters.SetPixelFormat(astiav.PixelFormatNv12)

	customOptions := astiav.NewDictionary()
	defer customOptions.Free()
	require.NoError(t, customOptions.Set("pix_fmt", "yuv420p", 0))

	c := &codecInternals{
		codec:        encoder,
		codecContext: codecContext,
	}

	require.NoError(t, c.setupPixelFormat(ctx, true, codecParameters, customOptions, nil))
	require.Equal(t, astiav.PixelFormatYuv420P, codecContext.PixelFormat())
}

func TestSetupPixelFormat_RawvideoDecoderKeepsDemuxerPixelFormatOverInputOption(t *testing.T) {
	ctx := context.Background()

	decoder := astiav.FindDecoder(astiav.CodecIDRawvideo)
	require.NotNil(t, decoder)

	codecContext := astiav.AllocCodecContext(decoder)
	require.NotNil(t, codecContext)
	defer codecContext.Free()

	codecParameters := astiav.AllocCodecParameters()
	require.NotNil(t, codecParameters)
	defer codecParameters.Free()
	codecParameters.SetMediaType(astiav.MediaTypeVideo)
	codecParameters.SetCodecID(astiav.CodecIDRawvideo)
	codecParameters.SetWidth(1920)
	codecParameters.SetHeight(1920)
	codecParameters.SetPixelFormat(astiav.PixelFormatNv21)

	customOptions := astiav.NewDictionary()
	defer customOptions.Free()
	require.NoError(t, customOptions.Set("pixel_format", "yuv420p", 0))

	c := &codecInternals{
		codec:        decoder,
		codecContext: codecContext,
	}

	require.NoError(t, c.setupPixelFormat(ctx, false, codecParameters, customOptions, nil))
	require.Equal(t, astiav.PixelFormatNv21, codecContext.PixelFormat())
}

func TestSetupPixelFormat_RawvideoDecoderUsesInputOptionWhenDemuxerPixelFormatUnknown(t *testing.T) {
	ctx := context.Background()

	decoder := astiav.FindDecoder(astiav.CodecIDRawvideo)
	require.NotNil(t, decoder)

	codecContext := astiav.AllocCodecContext(decoder)
	require.NotNil(t, codecContext)
	defer codecContext.Free()

	codecParameters := astiav.AllocCodecParameters()
	require.NotNil(t, codecParameters)
	defer codecParameters.Free()
	codecParameters.SetMediaType(astiav.MediaTypeVideo)
	codecParameters.SetCodecID(astiav.CodecIDRawvideo)
	codecParameters.SetWidth(1920)
	codecParameters.SetHeight(1920)
	codecParameters.SetPixelFormat(astiav.PixelFormatNone)

	customOptions := astiav.NewDictionary()
	defer customOptions.Free()
	require.NoError(t, customOptions.Set("pixel_format", "yuv420p", 0))

	c := &codecInternals{
		codec:        decoder,
		codecContext: codecContext,
	}

	require.NoError(t, c.setupPixelFormat(ctx, false, codecParameters, customOptions, nil))
	require.Equal(t, astiav.PixelFormatYuv420P, codecContext.PixelFormat())
}

func TestNewDecoder_RawvideoPreservesDemuxerNV21Parameters(t *testing.T) {
	ctx := context.Background()

	const rawvideoNV21CodecTag = astiav.CodecTag(0x3132564e)

	codecParameters := astiav.AllocCodecParameters()
	require.NotNil(t, codecParameters)
	defer codecParameters.Free()
	codecParameters.SetMediaType(astiav.MediaTypeVideo)
	codecParameters.SetCodecID(astiav.CodecIDRawvideo)
	codecParameters.SetCodecTag(rawvideoNV21CodecTag)
	codecParameters.SetWidth(1920)
	codecParameters.SetHeight(1920)
	codecParameters.SetPixelFormat(astiav.PixelFormatNv21)

	customOptions := astiav.NewDictionary()
	defer customOptions.Free()
	require.NoError(t, customOptions.Set("pixel_format", "yuv420p", 0))

	decoder, err := NewDecoder(ctx, DecoderInput{
		CodecParameters: codecParameters,
		CustomOptions:   customOptions,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, decoder.Close(ctx)) }()

	require.Equal(t, astiav.PixelFormatNv21, decoder.CodecContext(ctx).PixelFormat())

	out := astiav.AllocCodecParameters()
	require.NotNil(t, out)
	defer out.Free()
	require.NoError(t, decoder.ToCodecParameters(ctx, out))
	require.Equal(t, astiav.PixelFormatNv21, out.PixelFormat())
	require.Equal(t, rawvideoNV21CodecTag, out.CodecTag())
}
