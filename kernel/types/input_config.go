package types

import (
	"context"

	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type KernelHook interface {
	FireHook(ctx context.Context, input Abstract) error
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

	OnPostOpen KernelHook
	OnPreClose KernelHook
}
