// encoder_scaler_pixfmt.go covers the scaler-destination pixel-format
// selection used by streamEncoderLocked.prepareScaler. It exists as a
// separate, pure helper so the HW-pixfmt branch can be covered by
// deterministic unit tests without spinning up a real libavcodec
// encoder.
//
// Background:
//
// When an encoder is configured with hw_frames_ctx (e.g.
// av1_mediacodec / hevc_mediacodec on Android, or NVENC reusing an
// upstream cuvid hw_frames_ctx), CodecContext.PixelFormat() returns
// the *hardware* pixel format (AV_PIX_FMT_MEDIACODEC,
// AV_PIX_FMT_CUDA, ...). Setting that as the destination pixfmt on a
// software AVFrame and calling av_frame_get_buffer fails with
// EINVAL ("Invalid argument") because there is no SW layout for a
// hwaccel format -- the buffer must come from av_hwframe_get_buffer
// against an initialized hw_frames_ctx, not from av_frame_get_buffer.
// libswscale also cannot scale into a hwaccel format directly.
//
// The encoder hot path (EncoderFullLocked.SendFrame) already handles
// SW->HW upload via av_hwframe_transfer_data when the input frame
// pixel format differs from hardwarePixelFormat. So the scaler should
// produce a *software* frame in the hw_frames_ctx's sw_format -- that
// frame is then uploaded by the encoder.
//
// selectScaledFramePixelFormat encodes that rule.

package kernel

import "github.com/asticode/go-astiav"

// fallbackScaledFrameSWPixelFormat is used when the encoder reports a
// hardware pixel format but no hw_frames_ctx is attached yet (the
// encoder hasn't been initialized in HwFramesCtx mode, or the
// SoftwarePixelFormat field is unset). NV12 is the universal SW
// upload format for mediacodec, NVENC, VAAPI, and Videotoolbox
// hardware encoders -- every modern HW encoder accepts NV12 input via
// av_hwframe_transfer_data.
const fallbackScaledFrameSWPixelFormat = astiav.PixelFormatNv12

// isHardwarePixelFormat reports whether the format is a hwaccel
// pixfmt (i.e. AV_PIX_FMT_FLAG_HWACCEL is set in its descriptor).
// HW pixfmts cannot be backed by an av_frame_get_buffer SW
// allocation; they require av_hwframe_get_buffer against an
// initialized hw_frames_ctx.
func isHardwarePixelFormat(pixFmt astiav.PixelFormat) bool {
	desc := pixFmt.Descriptor()
	if desc == nil {
		return false
	}
	return desc.Flags().Has(astiav.PixelFormatDescriptorFlagHwAccel)
}

// selectScaledFramePixelFormat picks the destination pixel format for
// the scaler's intermediate ScaledFrame.
//
// Inputs:
//   - encoderPixFmt: the codec context's reported pixel format. May
//     be a SW pixfmt (HwDeviceCtx mode -- encoder uploads SW frames
//     internally) or a HW pixfmt (HwFramesCtx mode -- the codec
//     context is configured with hw_frames_ctx).
//   - hwFramesCtx: the encoder's hw_frames_ctx, if any. When non-nil
//     and encoderPixFmt is a HW pixfmt, the configured
//     SoftwarePixelFormat tells us the SW layout the HW upload path
//     expects.
//
// Output:
//   - For SW encoderPixFmt: encoderPixFmt unchanged. The scaler runs
//     SW->SW; the encoder then either consumes the SW frame directly
//     (HwDeviceCtx) or uploads SW->HW in SendFrame.
//   - For HW encoderPixFmt + non-nil hw_frames_ctx with a valid
//     SoftwarePixelFormat: the SW format from hw_frames_ctx. The
//     encoder will av_hwframe_transfer_data this SW frame into a HW
//     buffer in SendFrame.
//   - For HW encoderPixFmt + missing hw_frames_ctx info: NV12
//     fallback. NV12 is the universal HW upload format and matches
//     mediacodec/NVENC/VAAPI/Videotoolbox defaults.
func selectScaledFramePixelFormat(
	encoderPixFmt astiav.PixelFormat,
	hwFramesCtx *astiav.HardwareFramesContext,
) astiav.PixelFormat {
	if !isHardwarePixelFormat(encoderPixFmt) {
		return encoderPixFmt
	}
	if hwFramesCtx != nil {
		swFmt := hwFramesCtx.SoftwarePixelFormat()
		if swFmt != astiav.PixelFormatNone && !isHardwarePixelFormat(swFmt) {
			return swFmt
		}
	}
	return fallbackScaledFrameSWPixelFormat
}

// shouldBypassScaler reports whether the scaler+upload pipeline can be
// skipped and the input frame forwarded to the encoder verbatim.
//
// Bypass arms, in priority order:
//
//	(b) HW->HW passthrough: the encoder reports a hwaccel pixfmt and
//	    the input frame is already in the SAME hwaccel pixfmt (e.g.
//	    mediacodec decoder feeding a mediacodec encoder). SendFrame
//	    forwards the frame without going through libswscale (which
//	    cannot scale a hwaccel source). Dim mismatch is tolerated only
//	    for MediaCodec because the Android Surface composer absorbs
//	    producer/consumer dim mismatch via the shared
//	    dev_ctx->native_window — the prod DJI scenario is
//	    decoder=1920x1072 / encoder=1920x1080, which must still bypass.
//
//	(a) SW->{HW or SW} same-resolution: the input pixfmt matches the
//	    SW upload format selected by selectScaledFramePixelFormat, so
//	    the encoder uploads SW->HW via av_hwframe_transfer_data in
//	    SendFrame (or consumes the SW frame directly). Dim match IS
//	    required on this arm: a SW->HW upload path cannot rescale.
//
// Arm (b) MUST be checked BEFORE the dim-equality early-return so the
// Surface-composer dim absorption is preserved.
func shouldBypassScaler(
	inputPixFmt astiav.PixelFormat,
	inputW, inputH int,
	encoderPixFmt astiav.PixelFormat,
	encoderW, encoderH int,
	hwFramesCtx *astiav.HardwareFramesContext,
) bool {
	if isHardwarePixelFormat(encoderPixFmt) && inputPixFmt == encoderPixFmt {
		if inputW == encoderW && inputH == encoderH {
			return true
		}
		return encoderPixFmt == astiav.PixelFormatMediacodec
	}
	if inputW != encoderW || inputH != encoderH {
		return false
	}
	passthroughPixFmt := selectScaledFramePixelFormat(encoderPixFmt, hwFramesCtx)
	return inputPixFmt == passthroughPixFmt
}
