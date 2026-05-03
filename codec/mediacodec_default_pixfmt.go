// mediacodec_default_pixfmt.go isolates the pure decision used by
// setupPixelFormat to pick the default pix_fmt for a MediaCodec
// encoder when no explicit pix_fmt has been configured.
//
// Default to AV_PIX_FMT_MEDIACODEC (HW Surface passthrough) only when an
// upstream HWDeviceContext is reusable AND the recorded decoder dims
// match the encoder target — the mediacodec decoder never attaches an
// hw_frames_ctx, so dim-equality on the shared HWDeviceContext is the
// signal we have for HW→HW passthrough. The dataflow is: decoder emits
// MEDIACODEC frame with data[3]=AVMediaCodecBuffer*, encoder calls
// av_mediacodec_release_buffer to render into its input Surface — no
// hw_frames_ctx, no libswscale, no transfer_data required.
//
// All other cases (no HWDeviceContext, dim mismatch, camera-raw) fall
// through to NV12 + SW upload, since libswscale cannot consume hwaccel
// pixel formats and mediacodec hwctx returns ENOSYS for transfer_data.

package codec

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/resource"
)

// selectMediaCodecEncoderDefaultPixFmt returns the default pix_fmt to
// use for a MediaCodec encoder open-time options when no explicit
// pix_fmt has been configured.
//
//   - PixelFormatMediacodec: when reusable resources expose a
//     HWDeviceContext AND the recorded upstream decoder dims equal
//     the encoder target. HW→HW passthrough via the shared
//     dev_ctx->surface / dev_ctx->native_window: decoder emits
//     pix_fmt=MEDIACODEC frames with data[3]=AVMediaCodecBuffer*,
//     encoder calls av_mediacodec_release_buffer to render directly
//     into its input Surface. No hw_frames_ctx required — mediacodec
//     hwctx does not implement frames-level callbacks.
//   - PixelFormatNv12: every other case (no reusable HWDeviceContext,
//     dim mismatch, or zero recorded dims). Scaling is required (or
//     happens via the SW-upload path for camera-raw inputs) and
//     libswscale demands a SW source pixfmt; the encoder uploads
//     SW→HW internally via copy_frame_to_buffer.
//
// targetW/targetH are the encoder's configured target resolution
// (codecParameters.Width()/Height() at setupPixelFormat call site).
func selectMediaCodecEncoderDefaultPixFmt(
	reusableResources *resource.Resources,
	targetW, targetH int,
) astiav.PixelFormat {
	if reusableResources == nil || reusableResources.HWDeviceContext == nil {
		return astiav.PixelFormatNv12
	}
	// HWFramesContextWidth/Height are recorded unconditionally by
	// resourcesFromDecoder when HWDeviceContext is set. Zero values
	// mean the upstream decoder had no CodecContext yet (pre-open),
	// so fall back to NV12 to be safe.
	if reusableResources.HWFramesContextWidth == 0 || reusableResources.HWFramesContextHeight == 0 {
		return astiav.PixelFormatNv12
	}
	if reusableResources.HWFramesContextWidth != targetW ||
		reusableResources.HWFramesContextHeight != targetH {
		return astiav.PixelFormatNv12
	}
	return astiav.PixelFormatMediacodec
}
