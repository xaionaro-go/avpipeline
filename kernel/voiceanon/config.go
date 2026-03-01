// Package voiceanon provides a kernel that anonymizes voice in audio streams.
//
// The kernel applies pitch-shifting via FFmpeg's rubberband filter to make
// speakers unrecognizable while preserving speech content. It supports an
// optional speaker whitelist (via sherpa-onnx speaker embeddings) so that
// whitelisted speakers pass through unchanged.
package voiceanon

import (
	"fmt"
	"strings"
)

// Config configures the VoiceAnonymizer kernel.
type Config struct {
	// PitchScale controls the pitch shift factor for anonymization.
	// Values < 1.0 lower pitch; > 1.0 raise pitch.
	// Default: 0.7 (noticeably lower voice).
	PitchScale float64

	// FormantPreserve enables formant preservation during pitch shifting.
	// When true, the shifted voice sounds more natural (less chipmunk-like).
	// Default: true.
	FormantPreserve bool

	// SampleRate is the expected input sample rate (e.g. 44100, 48000).
	// Default: 0 (auto-detect from first frame).
	SampleRate int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PitchScale:      0.7,
		FormantPreserve: true,
	}
}

// FilterString builds the FFmpeg rubberband filter string.
// Example output: "rubberband=pitch=0.7:formant=preserved"
func (c *Config) FilterString() string {
	pitchScale := c.PitchScale
	if pitchScale == 0 {
		pitchScale = 0.7
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("pitch=%g", pitchScale))
	if c.FormantPreserve {
		parts = append(parts, "formant=preserved")
	}

	return "rubberband=" + strings.Join(parts, ":")
}
