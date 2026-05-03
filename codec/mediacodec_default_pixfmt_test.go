// mediacodec_default_pixfmt_test.go covers selectMediaCodecEncoderDefaultPixFmt
// — the pure decision lifted out of setupPixelFormat.
//
// Falsification check: replace the function body with a return of
// PixelFormatMediacodec and the dim-mismatch test below fails — proving
// the test discriminates the buggy default from the correct one.

package codec

import (
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/codec/resource"
)

func TestSelectMediaCodecEncoderDefaultPixFmt(t *testing.T) {
	// Sentinel non-nil pointers; helper only inspects nil-ness, never
	// dereferences. No FFmpeg context is allocated.
	hwDev := &astiav.HardwareDeviceContext{}
	hwFrames := &astiav.HardwareFramesContext{}

	t.Run("nil reusable resources -> NV12", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(nil, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWDeviceContext nil -> NV12", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWDeviceContext set, dims unrecorded -> NV12 (decoder pre-open)", func(t *testing.T) {
		// Recorded dims are 0,0 — resourcesFromDecoder hasn't observed
		// the decoder's CodecContext yet. Fall back to NV12 to avoid
		// gambling that decoder dims match encoder target.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext: hwDev,
		}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWDeviceContext + matching dims, HWFramesContext nil -> MEDIACODEC mediacodec passthrough", func(t *testing.T) {
		// The 1920x1920 mediacodec→mediacodec scenario. FFmpeg's
		// mediacodec decoder never attaches an hw_frames_ctx
		// (hwcontext_mediacodec.c only registers device_create/init/
		// uninit), so HWFramesContext is nil even on a fully-functional
		// Surface-mode pipeline. Recorded decoder dims still allow us
		// to route the encoder onto av_mediacodec_release_buffer
		// (Surface passthrough via the shared dev_ctx->native_window),
		// which is the only path that produces packets without
		// av_hwframe_transfer_data (mediacodec hwctx returns ENOSYS).
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1920,
		}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWFramesContext set, dims mismatch -> NV12 cross-resolution scaler", func(t *testing.T) {
		// Dim-mismatch (e.g. 1920x1080 source decoded by cuvid feeding a
		// 1920x1920 mediacodec encoder — synthetic but covers the
		// scaler invariant). MEDIACODEC would force libswscale to read
		// a hwaccel pixfmt and fail.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1080,
		}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWFramesContext set, width mismatch -> NV12", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1280,
			HWFramesContextHeight: 1920,
		}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWFramesContext set, dims match -> MEDIACODEC (HW->HW passthrough cuvid->nvenc analog)", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1920,
		}, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("zero target dims with zero recorded dims -> NV12 (degenerate but safe)", func(t *testing.T) {
		// Edge case: target dims unset (0). Recorded dims also 0
		// triggers the dims-unrecorded guard above and falls through to
		// NV12. Real call sites always pass non-zero target dims
		// (codecParameters.Width()/Height() set by amendVideoCodecParams).
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext: hwDev,
			HWFramesContext: hwFrames,
		}, 0, 0)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})
}
