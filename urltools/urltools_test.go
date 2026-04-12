package urltools

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsFileURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// File URLs
		{"", true},
		{"./video.mp4", true},
		{"/path/to/file.ts", true},
		{"file:///tmp/video.mp4", true},

		// Stream protocols used by ffstream/avd
		{"rtmp://server/app/stream", false},
		{"rtmps://server/app/stream", false},
		{"srt://server:9000", false},
		{"udp://239.0.0.1:1234", false},
		{"tcp://server:1234", false},
		{"http://server/stream", false},
		{"https://server/stream", false},
		{"rtsp://camera/stream", false},
		{"webrtc://server", false},

		// Unknown scheme defaults to file
		{"custom://something", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsFileURL(context.Background(), tc.input))
		})
	}
}

func TestFormatNameFromURL(t *testing.T) {
	tests := []struct {
		rawURL   string
		expected string
	}{
		// File extensions
		{"file:///tmp/video.mp4", "mp4"},
		{"file:///tmp/video.mkv", "matroska"},
		{"file:///tmp/video.flv", "flv"},
		{"file:///tmp/video.ts", "mpegts"},
		{"file:///tmp/video.avi", "avi"},
		{"file:///tmp/video.webm", "webm"},

		// MP4 variants
		{"file:///tmp/audio.m4a", "mp4"},
		{"file:///tmp/video.m4v", "mp4"},
		{"file:///tmp/video.mov", "mp4"},

		// Matroska variants
		{"file:///tmp/video.mk3d", "matroska"},
		{"file:///tmp/subtitles.mks", "matroska"},

		// MPEG-TS variants
		{"file:///tmp/video.mts", "mpegts"},
		{"file:///tmp/video.m2ts", "mpegts"},
		{"file:///tmp/video.mpeg", "mpegts"},
		{"file:///tmp/video.mpg", "mpegts"},
		{"file:///tmp/video.vob", "mpegts"},

		// Streaming protocols (used heavily by both ffstream and avd)
		{"rtmp://server/app/key", "flv"},
		{"rtmps://server/app/key", "flv"},
		{"srt://server:9000", "mpegts"},
		{"udp://239.0.0.1:1234", "mpegts"},
		{"tcp://server:1234", "mpegts"},
		{"http://server/stream", "mpegts"},
		{"https://server/stream", "mpegts"},
		{"rtsp://camera:554/stream", "rtsp"},
		{"webrtc://server/room", "webrtc"},

		// No scheme (file path)
		{"/tmp/video.mp4", "mp4"},
		{"./video.flv", "flv"},

		// Unknown extension
		{"file:///tmp/video.xyz", ""},

		// Unknown scheme
		{"ftp://server/file", ""},
	}
	for _, tc := range tests {
		t.Run(tc.rawURL, func(t *testing.T) {
			u, err := url.Parse(tc.rawURL)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, FormatNameFromURL(u))
		})
	}
}

func TestFormanNameFromFileExtension(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/video.mp4", "mp4"},
		{"/path/to/audio.m4a", "mp4"},
		{"/path/to/video.mkv", "matroska"},
		{"/path/to/video.flv", "flv"},
		{"/path/to/stream.ts", "mpegts"},
		{"/path/to/video.avi", "avi"},
		{"/path/to/video.webm", "webm"},
		{"/path/to/noext", ""},
		{"video.mp4", "mp4"},
		{".hidden", ""},
		{"file.with.dots.mp4", "mp4"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormanNameFromFileExtension(tc.path))
		})
	}
}

// ffstream uses FormatNameFromURL to determine output format for its SenderFactory.
// avd doesn't use urltools directly but kernel.Input/Output do internally.
func TestFormatNameFromURL_StreamingProtocols(t *testing.T) {
	// These are the primary protocols used by both ffstream and avd
	protocols := map[string]string{
		"rtmp://live.twitch.tv/app/live_key":   "flv",
		"rtmps://live.twitch.tv/app/live_key":  "flv",
		"srt://ingest.server:9000?streamid=s1": "mpegts",
		"udp://239.0.0.1:1234":                 "mpegts",
	}
	for rawURL, expectedFormat := range protocols {
		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		assert.Equal(t, expectedFormat, FormatNameFromURL(u), "URL: %s", rawURL)
	}
}
