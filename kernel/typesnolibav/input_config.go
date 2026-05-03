// input_config.go defines the InputConfig structure and lifecycle hooks for inputs.

package typesnolibav

import (
	"context"

	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Hook interface {
	FireHook(ctx context.Context, input Abstract) error
}

type HookFunc func(ctx context.Context, input Abstract) error

func (f HookFunc) FireHook(ctx context.Context, input Abstract) error {
	return f(ctx, input)
}

type InputConfig struct {
	CustomOptions  globaltypes.DictionaryItems
	RecvBufferSize uint
	AsyncOpen      bool
	AutoClose      bool

	// QuietOnOpenFailure demotes by-design open-failure log noise to
	// Debug. Set true for Inputs whose absence is a normal steady state
	// (e.g. an upstream rtmp publisher not yet connected, an empty
	// fallback priority slot). Default false preserves the legacy
	// WARN/ERRO levels.
	QuietOnOpenFailure bool

	// ForceRealTime is an implementation of slowing down the input to match real-time playback,
	// alternative to option "-re" in ffmpeg.
	ForceRealTime *bool
	ForceStartPTS *int64
	ForceStartDTS *int64

	DisplayRotation *float64
	AutoRotate      *bool

	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool

	OnPostOpen Hook
	OnPreClose Hook
}
