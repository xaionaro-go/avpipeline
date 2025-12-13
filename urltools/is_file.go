package urltools

import (
	"net/url"
)

func IsFileURL(urlString string) bool {
	url, err := url.Parse(urlString)
	if err != nil {
		return false
	}
	switch url.Scheme {
	case "file", "":
		return true
	case "rtmp", "rtmps", "srt", "udp", "tcp", "http", "https", "rtsp", "webrtc":
		return false
	default:
		return true
	}
}
