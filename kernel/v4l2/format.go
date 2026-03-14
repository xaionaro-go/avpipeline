// format.go defines V4L2 format constants and detection.

package v4l2

// IsV4L2Format reports whether the given format name refers to V4L2.
func IsV4L2Format(formatName string) bool {
	switch formatName {
	case "v4l2", "video4linux2":
		return true
	default:
		return false
	}
}
