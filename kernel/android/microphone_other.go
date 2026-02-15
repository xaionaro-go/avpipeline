//go:build !android || !cgo
// +build !android !cgo

// microphone_other.go provides a non-Android stub.

package android

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Microphone struct {
	*closuresignaler.ClosureSignaler
	Config        MicrophoneConfig
	Locker        xsync.Mutex
	formatContext *astiav.FormatContext
}

var _ types.Abstract = (*Microphone)(nil)

func NewMicrophone(ctx context.Context, cfg MicrophoneConfig) (*Microphone, error) {
	k := &Microphone{ClosureSignaler: closuresignaler.New(), Config: cfg, formatContext: astiav.AllocFormatContext()}
	if k.formatContext == nil {
		return nil, fmt.Errorf("unable to allocate format context")
	}
	internal.SetFinalizerFree(ctx, k.formatContext)
	return k, nil
}

func (k *Microphone) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Microphone) String() string {
	if k == nil {
		return "Microphone(<nil>)"
	}
	return "Microphone"
}

func (k *Microphone) WithOutputFormatContext(
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

func (k *Microphone) Close(ctx context.Context) error {
	_ = ctx
	if k == nil {
		return nil
	}
	k.ClosureSignaler.Close(ctx)
	return nil
}

func (k *Microphone) CloseChan() <-chan struct{} {
	if k == nil || k.ClosureSignaler == nil {
		return nil
	}
	return k.ClosureSignaler.CloseChan()
}

func (k *Microphone) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = input
	_ = outputCh
	return types.ErrUnexpectedInputType{}
}

func (k *Microphone) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = outputCh
	return fmt.Errorf("android microphone is only supported on android")
}
