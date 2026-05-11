//go:build !android || !cgo
// +build !android !cgo

package android

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Camera2NDK struct {
	*closuresignaler.ClosureSignaler
	Config        Camera2NDKConfig
	Locker        xsync.Mutex
	formatContext *astiav.FormatContext
}

var _ kerneltypes.Abstract = (*Camera2NDK)(nil)

func NewCamera2NDK(ctx context.Context, cfg Camera2NDKConfig) (*Camera2NDK, error) {
	cfg, err := normalizeCamera2NDKConfig(cfg)
	if err != nil {
		return nil, err
	}
	k := &Camera2NDK{
		ClosureSignaler: closuresignaler.New(),
		Config:          cfg,
		formatContext:   astiav.AllocFormatContext(),
	}
	if k.formatContext == nil {
		return nil, fmt.Errorf("unable to allocate format context")
	}
	internal.SetFinalizerFree(ctx, k.formatContext)
	return k, nil
}

func ListCamera2NDKMetadata(ctx context.Context) ([]Camera2NDKMetadata, error) {
	_ = ctx
	return nil, fmt.Errorf("android camera2 ndk metadata is only supported on android with cgo")
}

func (k *Camera2NDK) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Camera2NDK) String() string {
	if k == nil {
		return "Camera2NDK(<nil>)"
	}
	return "Camera2NDK"
}

func (k *Camera2NDK) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	if k == nil {
		return
	}
	k.Locker.Do(ctx, func() {
		callback(k.formatContext)
	})
}

func (k *Camera2NDK) Close(ctx context.Context) error {
	_ = ctx
	if k == nil {
		return nil
	}
	k.ClosureSignaler.Close(ctx)
	return nil
}

func (k *Camera2NDK) CloseChan() <-chan struct{} {
	if k == nil || k.ClosureSignaler == nil {
		return nil
	}
	return k.ClosureSignaler.CloseChan()
}

func (k *Camera2NDK) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = input
	_ = outputCh
	return kerneltypes.ErrUnexpectedInputType{}
}

func (k *Camera2NDK) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = outputCh
	return fmt.Errorf("android camera2 ndk is only supported on android with cgo")
}
