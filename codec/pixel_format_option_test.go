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
