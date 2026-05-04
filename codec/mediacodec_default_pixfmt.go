// mediacodec_default_pixfmt.go isolates the pure decision used by
// setupPixelFormat to pick the default pix_fmt for a MediaCodec
// encoder when no explicit pix_fmt has been configured.
//
// Default to AV_PIX_FMT_MEDIACODEC (HW Surface passthrough) when:
//   - an upstream HWDeviceContext is present AND
//   - that HWDeviceContext is a mediacodec one (mediacodec_init() rejects
//     non-mediacodec hw_device_ctx with EINVAL — cross-vendor cases
//     like cuvid->mediacodec must stay on NV12) AND
//   - the bitstream codec has an in-tree BSF for SPS-rewriting
//     (H.264, HEVC). AV1 + future codecs lack a rewriter, so the
//     Surface-passthrough wire format is unsafe today.
//
// The Android Surface composer (SurfaceFlinger via the shared
// dev_ctx->native_window) absorbs producer/consumer dim mismatch, so
// the encoder's launcher-configured target may differ from the
// decoder's post-crop dims (e.g. 1920x1080 vs 1920x1072) without
// breaking HW->HW passthrough — and a strict dim-equality gate would
// force the wedge: NV12 fallback then triggers
// av_hwframe_transfer_data against the mediacodec hwctx, which returns
// ENOSYS (FFmpeg's hwcontext_mediacodec.c does not implement
// frames_get_buffer).
//
// The dataflow is: decoder emits MEDIACODEC frame with
// data[3]=AVMediaCodecBuffer*, encoder calls
// av_mediacodec_release_buffer to render into its input Surface — no
// hw_frames_ctx, no libswscale, no transfer_data required.
//
// All other cases (no HWDeviceContext, non-mediacodec HW device,
// AV1/future codecs, camera-raw) fall through to NV12 + SW upload.

package codec

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// selectMediaCodecEncoderDefaultPixFmt returns the default pix_fmt to
// use for a MediaCodec encoder open-time options when no explicit
// pix_fmt has been configured.
//
// targetW/targetH are the encoder's configured target resolution
// (codecParameters.Width()/Height() at setupPixelFormat call site).
// They are kept in the signature for diagnostic logging at the call
// site (decision-time Debugf) and for future heuristics — not used
// to gate the HW-passthrough decision because the Surface composer
// absorbs producer/consumer dim mismatch on the shared
// dev_ctx->native_window.
func selectMediaCodecEncoderDefaultPixFmt(
	reusableResources *resource.Resources,
	codecID astiav.CodecID,
	_ int, _ int,
) astiav.PixelFormat {
	if reusableResources == nil || reusableResources.HWDeviceContext == nil {
		return astiav.PixelFormatNv12
	}
	// HWDeviceContext type guard: mediacodec_init() rejects non-mediacodec
	// hw_device_ctx with EINVAL. Without this check, cross-vendor cases
	// (e.g. cuvid->mediacodec) would route here and fail at encoder open.
	if reusableResources.HardwareDeviceType != globaltypes.HardwareDeviceTypeMediaCodec {
		return astiav.PixelFormatNv12
	}
	if reusableResources.HWFramesContextWidth == 0 || reusableResources.HWFramesContextHeight == 0 {
		return astiav.PixelFormatNv12
	}
	switch codecID {
	case astiav.CodecIDH264, astiav.CodecIDHevc:
		return astiav.PixelFormatMediacodec
	default:
		// AV1 + future codecs: keep on NV12 until each gains an
		// in-tree BSF for SPS-rewriting.
		return astiav.PixelFormatNv12
	}
}
