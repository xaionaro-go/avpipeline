package whisper

import (
	"fmt"
	"strings"
	"time"
)

// WhisperConfig configures the FFmpeg whisper audio filter for speech-to-text transcription.
type WhisperConfig struct {
	// Model is the path to the whisper.cpp model file (e.g. ggml-base.bin).
	// Required.
	Model string

	// Language sets the language for speech recognition.
	// Use "auto" for auto-detection. Whisper supports 99 languages.
	// Default: "auto".
	Language string

	// Queue is the audio buffer duration before triggering transcription.
	// Larger values improve accuracy but increase latency.
	// Default: 3s.
	Queue time.Duration

	// UseGPU enables GPU acceleration via whisper.cpp.
	// nil means use FFmpeg default (true).
	UseGPU *bool

	// GPUDevice selects the GPU device index.
	// nil means use FFmpeg default (0).
	GPUDevice *int

	// Destination is an optional output file or URL for transcription results.
	// Supports any FFmpeg AVIO protocol. Use "-" for stdout.
	Destination string

	// Format is the output format for Destination: "text", "srt", or "json".
	// Default: "text" (FFmpeg default).
	Format string

	// VADModel is the path to the Silero Voice Activity Detection model file.
	// When set, VAD is used to detect speech boundaries for lower latency.
	VADModel string

	// VADThreshold is the VAD detection sensitivity (0.0-1.0).
	// nil means use FFmpeg default (0.5).
	VADThreshold *float64

	// VADMinSpeechDuration is the minimum speech segment duration for VAD.
	// nil means use FFmpeg default (100ms).
	VADMinSpeechDuration *time.Duration

	// VADMinSilenceDuration is the minimum silence duration for VAD to split segments.
	// nil means use FFmpeg default (500ms).
	VADMinSilenceDuration *time.Duration
}

// DefaultWhisperConfig returns a WhisperConfig with sensible defaults.
// The Model field must still be set before use.
func DefaultWhisperConfig() *WhisperConfig {
	return &WhisperConfig{
		Language: "auto",
		Queue:    3 * time.Second,
	}
}

// FilterString builds the FFmpeg filter parameter string for the whisper filter.
// Example output: "whisper=model=/path/to/model.bin:language=auto:queue=3000000"
func (c *WhisperConfig) FilterString() string {
	var parts []string

	if c.Model != "" {
		parts = append(parts, "model="+escapeFilterValue(c.Model))
	}
	if c.Language != "" {
		parts = append(parts, "language="+escapeFilterValue(c.Language))
	}
	if c.Queue > 0 {
		parts = append(parts, fmt.Sprintf("queue=%d", c.Queue.Microseconds()))
	}
	if c.UseGPU != nil {
		if *c.UseGPU {
			parts = append(parts, "use_gpu=1")
		} else {
			parts = append(parts, "use_gpu=0")
		}
	}
	if c.GPUDevice != nil {
		parts = append(parts, fmt.Sprintf("gpu_device=%d", *c.GPUDevice))
	}
	if c.Destination != "" {
		parts = append(parts, "destination="+escapeFilterValue(c.Destination))
	}
	if c.Format != "" {
		parts = append(parts, "format="+escapeFilterValue(c.Format))
	}
	if c.VADModel != "" {
		parts = append(parts, "vad_model="+escapeFilterValue(c.VADModel))
	}
	if c.VADThreshold != nil {
		parts = append(parts, fmt.Sprintf("vad_threshold=%g", *c.VADThreshold))
	}
	if c.VADMinSpeechDuration != nil {
		parts = append(parts, fmt.Sprintf("vad_min_speech_duration=%d", c.VADMinSpeechDuration.Microseconds()))
	}
	if c.VADMinSilenceDuration != nil {
		parts = append(parts, fmt.Sprintf("vad_min_silence_duration=%d", c.VADMinSilenceDuration.Microseconds()))
	}

	return "whisper=" + strings.Join(parts, ":")
}

// escapeFilterValue escapes special characters in FFmpeg filter option values.
// The characters ':', '\', and ''' must be escaped with a leading '\'.
func escapeFilterValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
