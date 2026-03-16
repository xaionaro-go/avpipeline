//go:build android
// +build android

// microphone_config_android.go sets Android defaults and provides the
// InputPreset type backed by the real AAudio C enum.

package android

import (
	"time"

	aaudiocapi "github.com/AndroidGoLab/ndk/capi/aaudio"
)

// InputPreset represents the AAudio input preset for microphone capture routing.
type InputPreset = aaudiocapi.Aaudio_input_preset_t

const (
	InputPresetGeneric            InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_GENERIC
	InputPresetCamcorder          InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_CAMCORDER
	InputPresetVoiceRecognition   InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_VOICE_RECOGNITION
	InputPresetVoiceCommunication InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_VOICE_COMMUNICATION
	InputPresetUnprocessed        InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_UNPROCESSED
	InputPresetVoicePerformance   InputPreset = aaudiocapi.AAUDIO_INPUT_PRESET_VOICE_PERFORMANCE
)

const (
	microphoneDefaultSampleRate    = 48000
	microphoneDefaultChannels      = 1
	microphoneDefaultFrameSamples  = 1024
	microphoneDefaultBufferSamples = 4096
	microphoneDefaultPollInterval  = 10 * time.Millisecond
	microphoneDefaultInputPreset   = InputPresetUnprocessed
)
