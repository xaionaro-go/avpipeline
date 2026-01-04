// pixel_format.go defines the PixelFormat type for codecs.

package types

type PixelFormat string

func (pf PixelFormat) String() string {
	return string(pf)
}

const (
	PixelFormatUnknown  PixelFormat = "unknown"
	PixelFormatNV12     PixelFormat = "nv12"
	PixelFormatYUV420P  PixelFormat = "yuv420p"
	PixelFormatRGB24    PixelFormat = "rgb24"
	PixelFormatBGR24    PixelFormat = "bgr24"
	PixelFormatARGB     PixelFormat = "argb"
	PixelFormatABGR     PixelFormat = "abgr"
	PixelFormatRGBA     PixelFormat = "rgba"
	PixelFormatBGRA     PixelFormat = "bgra"
	PixelFormatUYVY     PixelFormat = "uyvy"
	PixelFormatYUY2     PixelFormat = "yuy2"
	PixelFormatGray8    PixelFormat = "gray8"
	PixelFormatMJPEG    PixelFormat = "mjpeg"
	PixelFormatH264     PixelFormat = "h264"
	PixelFormatHEVC     PixelFormat = "hevc"
	PixelFormatVP8      PixelFormat = "vp8"
	PixelFormatVP9      PixelFormat = "vp9"
	PixelFormatAV1      PixelFormat = "av1"
	PixelFormatDepth16U PixelFormat = "depth16u"
	PixelFormatDepth32F PixelFormat = "depth32f"
	PixelFormatY16      PixelFormat = "y16"
)
