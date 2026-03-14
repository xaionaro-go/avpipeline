//go:build !android
// +build !android

// microphone_config_other.go provides stub types for non-Android builds.

package android

// InputPreset is a stub for the AAudio input preset enum on non-Android platforms.
type InputPreset = int32

const (
	InputPresetGeneric            InputPreset = 1
	InputPresetCamcorder          InputPreset = 5
	InputPresetVoiceRecognition   InputPreset = 6
	InputPresetVoiceCommunication InputPreset = 7
	InputPresetUnprocessed        InputPreset = 9
	InputPresetVoicePerformance   InputPreset = 10
)
