package urltools

import (
	"net/url"
	"path/filepath"
	"slices"
)

func FormatNameFromURL(url *url.URL) string {
	switch url.Scheme {
	case "file", "":
		return FormanNameFromFileExtension(url.Path)
	case "rtmp", "rtmps":
		return "flv"
	case "srt":
		return "mpegts"
	case "udp", "tcp":
		return "mpegts"
	case "http", "https":
		return "mpegts"
	case "rtsp":
		return "rtsp"
	case "webrtc":
		return "webrtc"
	default:
		return ""
	}
}

func FormanNameFromFileExtension(path string) string {
	switch {
	case hasFileExtension(path, ".mp4", ".m4a", ".m4v", ".mov"):
		return "mp4"
	case hasFileExtension(path, ".mkv", ".mk3d", ".mks"):
		return "matroska"
	case hasFileExtension(path, ".flv"):
		return "flv"
	case hasFileExtension(path, ".ts", ".mts", ".m2ts", ".mpeg", ".mpg", ".vob"):
		return "mpegts"
	case hasFileExtension(path, ".avi"):
		return "avi"
	case hasFileExtension(path, ".webm"):
		return "webm"
	default:
		return ""
	}
}

func hasFileExtension(path string, exts ...string) bool {
	return slices.Contains(exts, filepath.Ext(path))
}
