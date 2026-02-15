//go:build !android
// +build !android

// microphone_config_other.go keeps defaults for non-Android builds.

package android

import "time"

const (
	microphoneDefaultSampleRate    = 48000
	microphoneDefaultChannels      = 1
	microphoneDefaultFrameSamples  = 1024
	microphoneDefaultBufferSamples = 4096
	microphoneDefaultPollInterval  = 10 * time.Millisecond
)
