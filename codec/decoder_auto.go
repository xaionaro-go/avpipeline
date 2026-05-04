// decoder_auto.go provides codec_id → preferred-hardware-decoder-name
// resolution for NaiveDecoderFactory's auto-select path.

package codec

import (
	"context"

	"github.com/asticode/go-astiav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// preferredHWDecoderName returns the registered hwaccel decoder name for the
// given codec_id under the requested hardware device type IFF that decoder
// is registered in libav, else "" (libav default).
//
// The candidate name is derived generically via Name.hwName — there is no
// per-codec allowlist. This lets the auto-select path pick av1_cuvid /
// h264_cuvid / hevc_cuvid for AV1/H264/HEVC streams on CUDA hosts while
// transparently extending to other registered hwaccel variants (e.g.
// vp9_cuvid, mjpeg_cuvid) and other hardware backends (qsv, vaapi, …)
// without further code changes.
//
// hwType==HardwareDeviceTypeNone defaults to CUDA for backward-compat with
// the avd cascade transcoder, which historically left hardware_device_type
// unset and relied on this helper picking av1_cuvid for AV1 publishers.
// Callers that want strictly-no-HW resolution must not enable
// AutoSelectHardwareDecoder.
//
// The FindDecoderByName guard preserves graceful degradation: if libav is
// compiled without the requested hwaccel decoder, "" is returned and the
// caller falls back to libav's default selection.
func preferredHWDecoderName(
	ctx context.Context,
	id astiav.CodecID,
	hwType HardwareDeviceType,
) Name {
	name, _ := preferredHWDecoderNameAndHardwareDeviceType(ctx, id, hwType)
	return name
}

func preferredHWDecoderNameAndHardwareDeviceType(
	ctx context.Context,
	id astiav.CodecID,
	hwType HardwareDeviceType,
) (Name, HardwareDeviceType) {
	if hwType == globaltypes.HardwareDeviceTypeNone {
		hwType = globaltypes.HardwareDeviceTypeCUDA
	}
	base := Name(id.Name())
	if base == "" {
		return "", globaltypes.HardwareDeviceTypeNone
	}
	candidate := base.hwName(ctx, false, hwType)
	if astiav.FindDecoderByName(string(candidate)) == nil {
		return "", globaltypes.HardwareDeviceTypeNone
	}
	return candidate, hwType
}
