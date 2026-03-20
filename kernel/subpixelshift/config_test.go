package subpixelshift

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColorModeString(t *testing.T) {
	tests := []struct {
		mode     ColorMode
		expected string
	}{
		{ColorModeAuto, "Auto"},
		{ColorModeYUV, "YUV"},
		{ColorModeRGB, "RGB"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.String())
		})
	}
}

func TestColorModeStringUnknown(t *testing.T) {
	unknown := ColorMode(99)
	s := unknown.String()
	assert.Contains(t, s, "99")
}

func TestMotionModeString(t *testing.T) {
	tests := []struct {
		mode     MotionMode
		expected string
	}{
		{MotionModeAuto, "Auto"},
		{MotionModeGlobal, "Global"},
		{MotionModePerBlock, "PerBlock"},
		{MotionModePerPixel, "PerPixel"},
		{MotionModeCodecMVs, "CodecMVs"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.String())
		})
	}
}

func TestMotionModeStringUnknown(t *testing.T) {
	unknown := MotionMode(99)
	s := unknown.String()
	assert.Contains(t, s, "99")
}

func TestStartupModeString(t *testing.T) {
	tests := []struct {
		mode     StartupMode
		expected string
	}{
		{StartupModePassthrough, "Passthrough"},
		{StartupModeBuffer, "Buffer"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.String())
		})
	}
}

func TestStartupModeStringUnknown(t *testing.T) {
	unknown := StartupMode(99)
	s := unknown.String()
	assert.Contains(t, s, "99")
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)

	assert.Equal(t, int32(2), cfg.Scale)
	assert.Equal(t, int32(8), cfg.BufferSize)
	assert.Equal(t, ColorModeAuto, cfg.ColorMode)
	assert.Equal(t, MotionModeAuto, cfg.MotionMode)
	assert.Equal(t, int32(16), cfg.BlockSize)
	assert.Equal(t, StartupModePassthrough, cfg.StartupMode)
}

func TestWithScale(t *testing.T) {
	cfg := DefaultConfig()
	WithScale(4).apply(cfg)
	assert.Equal(t, int32(4), cfg.Scale)
	// Other fields unchanged.
	assert.Equal(t, int32(8), cfg.BufferSize)
}

func TestWithBufferSize(t *testing.T) {
	cfg := DefaultConfig()
	WithBufferSize(16).apply(cfg)
	assert.Equal(t, int32(16), cfg.BufferSize)
	assert.Equal(t, int32(2), cfg.Scale)
}

func TestWithColorMode(t *testing.T) {
	cfg := DefaultConfig()
	WithColorMode(ColorModeRGB).apply(cfg)
	assert.Equal(t, ColorModeRGB, cfg.ColorMode)
	assert.Equal(t, MotionModeAuto, cfg.MotionMode)
}

func TestWithMotionMode(t *testing.T) {
	cfg := DefaultConfig()
	WithMotionMode(MotionModePerPixel).apply(cfg)
	assert.Equal(t, MotionModePerPixel, cfg.MotionMode)
	assert.Equal(t, ColorModeAuto, cfg.ColorMode)
}

func TestWithBlockSize(t *testing.T) {
	cfg := DefaultConfig()
	WithBlockSize(32).apply(cfg)
	assert.Equal(t, int32(32), cfg.BlockSize)
	assert.Equal(t, int32(8), cfg.BufferSize) // unchanged
}

func TestWithStartupMode(t *testing.T) {
	cfg := DefaultConfig()
	WithStartupMode(StartupModeBuffer).apply(cfg)
	assert.Equal(t, StartupModeBuffer, cfg.StartupMode)
	assert.Equal(t, int32(2), cfg.Scale)
}

func TestOptionsConfig(t *testing.T) {
	opts := Options{
		WithScale(3),
		WithBufferSize(12),
		WithColorMode(ColorModeYUV),
		WithMotionMode(MotionModeGlobal),
		WithBlockSize(8),
		WithStartupMode(StartupModeBuffer),
	}
	cfg := opts.Config()

	assert.Equal(t, int32(3), cfg.Scale)
	assert.Equal(t, int32(12), cfg.BufferSize)
	assert.Equal(t, ColorModeYUV, cfg.ColorMode)
	assert.Equal(t, MotionModeGlobal, cfg.MotionMode)
	assert.Equal(t, int32(8), cfg.BlockSize)
	assert.Equal(t, StartupModeBuffer, cfg.StartupMode)
}
