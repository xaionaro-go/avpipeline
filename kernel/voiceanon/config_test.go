package voiceanon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 0.7, cfg.PitchScale)
	assert.True(t, cfg.FormantPreserve)
	assert.Equal(t, 0, cfg.SampleRate)
}

func TestConfig_FilterString_Default(t *testing.T) {
	cfg := DefaultConfig()
	s := cfg.FilterString()
	assert.Equal(t, "rubberband=pitch=0.7:formant=preserved", s)
}

func TestConfig_FilterString_CustomPitch(t *testing.T) {
	cfg := Config{PitchScale: 1.3, FormantPreserve: true}
	s := cfg.FilterString()
	assert.Equal(t, "rubberband=pitch=1.3:formant=preserved", s)
}

func TestConfig_FilterString_NoFormant(t *testing.T) {
	cfg := Config{PitchScale: 0.5, FormantPreserve: false}
	s := cfg.FilterString()
	assert.Equal(t, "rubberband=pitch=0.5", s)
}

func TestConfig_FilterString_ZeroPitchUsesDefault(t *testing.T) {
	cfg := Config{PitchScale: 0, FormantPreserve: true}
	s := cfg.FilterString()
	assert.Equal(t, "rubberband=pitch=0.7:formant=preserved", s)
}
