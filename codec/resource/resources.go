// resources.go defines the Resources structure for holding codec resources.

package resource

import (
	"github.com/asticode/go-astiav"
)

// Resources captures hardware-side state that an encoder may borrow from the
// upstream decoder to avoid an unnecessary GPU upload/download round-trip.
//
// HWFramesContext, when non-nil, is the upstream decoder's hw_frames_ctx.
// Encoder may reuse it only when its expected dims and formats match the
// encoder's required values. Population is lazy: cuvid allocates the
// hw_frames_ctx after the first decoded frame; if the encoder is created
// before that first frame, HWFramesContext stays nil and the encoder falls
// back to HWDeviceContext-only mode (existing behaviour).
type Resources struct {
	HWDeviceContext         *astiav.HardwareDeviceContext
	HWFramesContext         *astiav.HardwareFramesContext
	HWFramesContextWidth    int
	HWFramesContextHeight   int
	HWFramesContextHWPixFmt astiav.PixelFormat
}
