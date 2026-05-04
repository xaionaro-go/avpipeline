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
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestSelectMediaCodecEncoderDefaultPixFmt(t *testing.T) {
	// Sentinel non-nil pointers; helper only inspects nil-ness, never
	// dereferences. No FFmpeg context is allocated.
	hwDev := &astiav.HardwareDeviceContext{}
	hwFrames := &astiav.HardwareFramesContext{}

	t.Run("nil reusable resources -> NV12", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(nil, astiav.CodecIDH264, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWDeviceContext nil -> NV12", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{}, astiav.CodecIDH264, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("HWDeviceContext set, dims unrecorded -> NV12 (decoder pre-open)", func(t *testing.T) {
		// Recorded dims are 0,0 — resourcesFromDecoder hasn't observed
		// the decoder's CodecContext yet. Fall back to NV12 to avoid
		// gambling that decoder dims match encoder target.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:    hwDev,
			HardwareDeviceType: globaltypes.HardwareDeviceTypeMediaCodec,
		}, astiav.CodecIDH264, 1920, 1920)
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
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1920,
		}, astiav.CodecIDH264, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWFramesContext set, dim MISmatch + H264 -> MEDIACODEC (Surface composer absorbs)", func(t *testing.T) {
		// The prod DJI scenario: decoder post-crop dims 1920x1072,
		// encoder launcher-configured 1920x1080. SurfaceFlinger absorbs
		// producer/consumer dim mismatch on the shared
		// dev_ctx->native_window, so HW->HW passthrough is safe.
		// The strict dim-equality gate forced NV12 fallback, which then
		// triggered av_hwframe_transfer_data against the mediacodec
		// hwctx -> ENOSYS, wedging the encoder.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1072,
		}, astiav.CodecIDH264, 1920, 1080)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWFramesContext set, width mismatch + H264 -> MEDIACODEC (Surface composer absorbs)", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1280,
			HWFramesContextHeight: 1920,
		}, astiav.CodecIDH264, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWFramesContext set, dims match + H264 -> MEDIACODEC (HW->HW passthrough)", func(t *testing.T) {
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContext:       hwFrames,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1920,
		}, astiav.CodecIDH264, 1920, 1920)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWDeviceContext + matching dims + HEVC -> MEDIACODEC", func(t *testing.T) {
		// HEVC has the same in-tree SPS/VPS rewriter support as H.264,
		// so the Surface-passthrough path is safe.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1080,
		}, astiav.CodecIDHevc, 1920, 1080)
		assert.Equal(t, astiav.PixelFormatMediacodec, got)
	})

	t.Run("HWDeviceContext + matching dims + AV1 -> NV12 (no in-tree SPS rewriter)", func(t *testing.T) {
		// AV1 lacks an in-tree BSF for SPS-rewriting, so HW->HW
		// passthrough via the Surface composer is unsafe today. Stay on
		// NV12 + SW upload until each new codec gains a rewriter.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1080,
		}, astiav.CodecIDAv1, 1920, 1080)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("non-mediacodec HWDeviceContext + matching dims + H264 -> NV12 (cross-vendor guard)", func(t *testing.T) {
		// cuvid->mediacodec route: the upstream HWDeviceContext is CUDA,
		// not mediacodec. mediacodec_init() rejects non-mediacodec
		// hw_device_ctx with EINVAL, so we must NOT route the encoder
		// onto pix_fmt=MEDIACODEC. NV12 + SW upload is the only safe
		// option.
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:       hwDev,
			HardwareDeviceType:    globaltypes.HardwareDeviceTypeCUDA,
			HWFramesContextWidth:  1920,
			HWFramesContextHeight: 1080,
		}, astiav.CodecIDH264, 1920, 1080)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})

	t.Run("zero target dims with zero recorded dims -> NV12 (degenerate but safe)", func(t *testing.T) {
		// Edge case: target dims unset (0). Recorded dims also 0
		// triggers the dims-unrecorded guard above and falls through to
		// NV12. Real call sites always pass non-zero target dims
		// (codecParameters.Width()/Height() set by amendVideoCodecParams).
		got := selectMediaCodecEncoderDefaultPixFmt(&resource.Resources{
			HWDeviceContext:    hwDev,
			HardwareDeviceType: globaltypes.HardwareDeviceTypeMediaCodec,
			HWFramesContext:    hwFrames,
		}, astiav.CodecIDH264, 0, 0)
		assert.Equal(t, astiav.PixelFormatNv12, got)
	})
}
