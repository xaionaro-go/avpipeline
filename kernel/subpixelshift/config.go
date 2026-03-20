package subpixelshift

import "fmt"

// ColorMode specifies how input frames are processed for sub-pixel analysis.
type ColorMode int32

const (
	ColorModeAuto ColorMode = iota
	ColorModeYUV
	ColorModeRGB
)

func (m ColorMode) String() string {
	switch m {
	case ColorModeAuto:
		return "Auto"
	case ColorModeYUV:
		return "YUV"
	case ColorModeRGB:
		return "RGB"
	default:
		return fmt.Sprintf("ColorMode(%d)", m)
	}
}

// MotionMode specifies the motion estimation strategy.
type MotionMode int32

const (
	MotionModeAuto MotionMode = iota
	MotionModeGlobal
	MotionModePerBlock
	MotionModePerPixel
	MotionModeCodecMVs
)

func (m MotionMode) String() string {
	switch m {
	case MotionModeAuto:
		return "Auto"
	case MotionModeGlobal:
		return "Global"
	case MotionModePerBlock:
		return "PerBlock"
	case MotionModePerPixel:
		return "PerPixel"
	case MotionModeCodecMVs:
		return "CodecMVs"
	default:
		return fmt.Sprintf("MotionMode(%d)", m)
	}
}

// StartupMode specifies behavior before the ring buffer is full.
type StartupMode int32

const (
	StartupModePassthrough StartupMode = iota
	StartupModeBuffer
)

func (m StartupMode) String() string {
	switch m {
	case StartupModePassthrough:
		return "Passthrough"
	case StartupModeBuffer:
		return "Buffer"
	default:
		return fmt.Sprintf("StartupMode(%d)", m)
	}
}

// Config holds all parameters for the sub-pixel shift upscaling kernel.
type Config struct {
	Scale       int32
	BufferSize  int32
	ColorMode   ColorMode
	MotionMode  MotionMode
	BlockSize   int32
	StartupMode StartupMode
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Scale:       2,
		BufferSize:  8,
		ColorMode:   ColorModeAuto,
		MotionMode:  MotionModeAuto,
		BlockSize:   16,
		StartupMode: StartupModePassthrough,
	}
}

// Option configures a Config.
type Option interface {
	apply(*Config)
}

// Options is a slice of Option.
type Options []Option

func (opts Options) apply(cfg *Config) {
	for _, o := range opts {
		o.apply(cfg)
	}
}

// Config returns a Config with defaults overridden by opts.
func (opts Options) Config() Config {
	cfg := DefaultConfig()
	opts.apply(cfg)
	return *cfg
}

type optionScale int32

func (o optionScale) apply(cfg *Config) { cfg.Scale = int32(o) }

// WithScale sets the upscaling factor (e.g. 2 for 2x).
func WithScale(v int32) Option { return optionScale(v) }

type optionBufferSize int32

func (o optionBufferSize) apply(cfg *Config) { cfg.BufferSize = int32(o) }

// WithBufferSize sets the ring buffer capacity in frames.
func WithBufferSize(v int32) Option { return optionBufferSize(v) }

type optionColorMode ColorMode

func (o optionColorMode) apply(cfg *Config) { cfg.ColorMode = ColorMode(o) }

// WithColorMode sets the color processing mode.
func WithColorMode(v ColorMode) Option { return optionColorMode(v) }

type optionMotionMode MotionMode

func (o optionMotionMode) apply(cfg *Config) { cfg.MotionMode = MotionMode(o) }

// WithMotionMode sets the motion estimation strategy.
func WithMotionMode(v MotionMode) Option { return optionMotionMode(v) }

type optionBlockSize int32

func (o optionBlockSize) apply(cfg *Config) { cfg.BlockSize = int32(o) }

// WithBlockSize sets the block size for per-block motion estimation.
func WithBlockSize(v int32) Option { return optionBlockSize(v) }

type optionStartupMode StartupMode

func (o optionStartupMode) apply(cfg *Config) { cfg.StartupMode = StartupMode(o) }

// WithStartupMode sets the startup behavior mode.
func WithStartupMode(v StartupMode) Option { return optionStartupMode(v) }
