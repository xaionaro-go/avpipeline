// encoder_scaler_descriptors.go validates the source/destination descriptor
// pair fed into libswscale (`scaler.NewSoftware` →
// `astiav.CreateSoftwareScaleContext` → `sws_getContext`).
//
// Background:
//
// `sws_getContext` returns NULL — surfaced by astiav as
// `errors.New("astiav: empty new context")` — whenever the requested
// source or destination descriptor is something libswscale cannot back:
// non-positive width/height, AV_PIX_FMT_NONE, or a hwaccel pixel
// format on the SW path (libswscale cannot scale into / out of a
// hwaccel format directly — the encoder hot path is responsible for
// any HW upload via av_hwframe_transfer_data, see
// encoder_scaler_pixfmt.go).
//
// Diagnostic, not workaround. The validator returns a typed error
// with the exact field that is invalid, so the prepareScaler caller
// can log a single message naming the cause instead of the opaque
// "empty new context" pass-through. The error remains a hard failure
// — we do not paper over invalid descriptors by guessing replacement
// values, because doing so would mask the upstream bug that produced
// them.

package kernel

import (
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

// ErrInvalidScalerDescriptors is returned by validateScalerDescriptors
// when the source/destination W/H/PixelFormat tuple cannot back a
// libswscale context. The wrapped Reasons list names every offending
// field so log readers can immediately see whether the source or the
// destination side is the culprit, without re-instrumenting and
// re-deploying.
type ErrInvalidScalerDescriptors struct {
	SrcResolution codectypes.Resolution
	SrcPixFmt     astiav.PixelFormat
	DstResolution codectypes.Resolution
	DstPixFmt     astiav.PixelFormat
	Reasons       []string
}

func (e ErrInvalidScalerDescriptors) Error() string {
	return fmt.Sprintf(
		"invalid scaler descriptors src=%dx%d/%s dst=%dx%d/%s: %s",
		e.SrcResolution.Width, e.SrcResolution.Height, e.SrcPixFmt,
		e.DstResolution.Width, e.DstResolution.Height, e.DstPixFmt,
		strings.Join(e.Reasons, "; "),
	)
}

// validateScalerDescriptors checks the source/destination pair fed
// into libswscale. Returns nil when sws_getContext can be expected to
// succeed for the given tuple; otherwise returns
// ErrInvalidScalerDescriptors enumerating every offending field.
//
// Each precondition mirrors a documented sws_getContext failure mode
// [T1: ffmpeg/libswscale/utils.c sws_init_context]:
//
//   - Width and height must be strictly positive on both sides.
//     Zero/negative dims feed into integer math that returns NULL.
//   - PixelFormat AV_PIX_FMT_NONE is rejected: libswscale cannot
//     guess a layout and bails immediately.
//   - Hardware-accelerated pixel formats are rejected on both sides:
//     libswscale operates on SW frames; HW frames must be transferred
//     via av_hwframe_transfer_data before any scaling. The encoder
//     path's selectScaledFramePixelFormat already maps HW dst pixfmts
//     to a SW upload format, so a HW value here means that mapping
//     was bypassed — caller should investigate, not silently coerce.
func validateScalerDescriptors(
	srcRes codectypes.Resolution,
	srcPixFmt astiav.PixelFormat,
	dstRes codectypes.Resolution,
	dstPixFmt astiav.PixelFormat,
) error {
	var reasons []string

	if srcRes.Width == 0 || srcRes.Height == 0 {
		reasons = append(reasons, "source resolution has a zero dimension")
	}
	if dstRes.Width == 0 || dstRes.Height == 0 {
		reasons = append(reasons, "destination resolution has a zero dimension")
	}
	if srcPixFmt == astiav.PixelFormatNone {
		reasons = append(reasons, "source pixel format is AV_PIX_FMT_NONE")
	}
	if dstPixFmt == astiav.PixelFormatNone {
		reasons = append(reasons, "destination pixel format is AV_PIX_FMT_NONE")
	}
	if srcPixFmt != astiav.PixelFormatNone && isHardwarePixelFormat(srcPixFmt) {
		reasons = append(reasons, fmt.Sprintf("source pixel format %s is a hardware pixfmt that libswscale cannot consume directly", srcPixFmt))
	}
	if dstPixFmt != astiav.PixelFormatNone && isHardwarePixelFormat(dstPixFmt) {
		reasons = append(reasons, fmt.Sprintf("destination pixel format %s is a hardware pixfmt that libswscale cannot produce directly", dstPixFmt))
	}

	if len(reasons) == 0 {
		return nil
	}
	return ErrInvalidScalerDescriptors{
		SrcResolution: srcRes,
		SrcPixFmt:     srcPixFmt,
		DstResolution: dstRes,
		DstPixFmt:     dstPixFmt,
		Reasons:       reasons,
	}
}
