// Package urltools provides utilities for parsing and analyzing URLs in the context of AV streams.
//
// is_file.go provides a function to determine if a URL refers to a local file.
package urltools

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/avpipeline/logger"
)

func IsFileURL(urlString string) (_ret bool) {
	ctx := context.TODO()
	logger.Tracef(ctx, "IsFileURL: %s", urlString)
	defer func() { logger.Tracef(ctx, "/IsFileURL: %s: %v", urlString, _ret) }()
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
