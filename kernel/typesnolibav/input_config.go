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

	// ForceRealTime is an implementation of slowing down the input to match real-time playback,
	// alternative to option "-re" in ffmpeg.
	ForceRealTime *bool
	ForceStartPTS *int64
	ForceStartDTS *int64

	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool

	OnPostOpen Hook
	OnPreClose Hook
}
