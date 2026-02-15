// microphone_config.go defines configuration for Android microphone capture.

package android

import (
	"time"

	"github.com/asticode/go-astiav"
)

type MicrophoneConfig struct {
	DeviceName    string
	LibraryPath   string
	SampleRate    int
	Channels      int
	SampleFormat  astiav.SampleFormat
	FrameSamples  int
	BufferSamples int
	PollInterval  time.Duration
}
