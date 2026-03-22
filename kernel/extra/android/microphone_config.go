// microphone_config.go defines configuration for Android microphone capture.

package android

import (
	"time"
)

type MicrophoneConfig struct {
	// DeviceID selects the AAudio capture device. When nil, AAudio
	// picks the default device. When set (even to 0), the value is
	// passed to AAudioStreamBuilder_setDeviceId.
	//
	// The device ID corresponds to the "Port ID" in Android's audio
	// policy. To list available input devices and their IDs:
	//
	//   dumpsys media.audio_policy | sed -n '/Available input devices/,/^$/p'
	//
	// To find the device ID for a specific device by name:
	//
	//   dumpsys media.audio_policy | awk '/Available input devices/,/^$/{print}' | awk '/Port ID/{id=$4; gsub(/;/,"",id)} /DEVICE_NAME/{print id}'
	//
	// Note: AAudio silently falls back to the default device when
	// a non-existent device ID is specified.
	DeviceID *int32

	// DeviceNamePattern selects the capture device by matching a regexp
	// against device names from `dumpsys media.audio_policy`. Ignored
	// when DeviceID is set. The pattern must match exactly one device;
	// if zero or multiple devices match, NewMicrophone returns an error
	// listing available devices.
	DeviceNamePattern string

	SampleRate    int
	Channels      int
	FrameSamples  int
	BufferSamples int
	PollInterval  time.Duration
	InputPreset   InputPreset

	// DisableSensorPrivacyOnStart controls whether to run
	// `cmd sensor_privacy disable 0 microphone` before opening
	// the capture device. Requires root. Errors are logged but
	// do not prevent capture from starting.
	DisableSensorPrivacyOnStart bool

	// DisableSensorPrivacyOnSilence controls whether to run
	// `cmd sensor_privacy disable 0 microphone` when complete
	// silence (all-zero samples) is detected for over 1 second.
	// Requires root. Errors are logged but do not stop capture.
	DisableSensorPrivacyOnSilence bool
}
