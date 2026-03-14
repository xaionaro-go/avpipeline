// video_device.go defines the VideoDevice type representing a V4L2 device.

package v4l2

// VideoDevice represents a single V4L2 video device entry.
type VideoDevice struct {
	Path string // e.g., "/dev/video0"
	Name string // e.g., "USB Video: DJI Osmo Pocket 3"
}
