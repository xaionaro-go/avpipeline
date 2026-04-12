// kernel.go implements a client-side kernel.

// Package client provides a client-side kernel implementation.
package client

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Kernel struct{}

var (
	_ kernel.Abstract = (*Kernel)(nil)
	_ packet.Source   = (*Kernel)(nil)
	_ packet.Sink     = (*Kernel)(nil)
)

func (k *Kernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Kernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return kernel.ErrNotImplemented{}
}

func (k *Kernel) String() string {
	return "client.Kernel"
}

func (k *Kernel) Close(ctx context.Context) error {
	return kernel.ErrNotImplemented{}
}

func (k *Kernel) CloseChan() <-chan struct{} {
	return nil
}

func (k *Kernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return kernel.ErrNotImplemented{}
}

func (k *Kernel) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Warnf(ctx, "WithOutputFormatContext is not implemented for client.Kernel")
}

func (k *Kernel) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Warnf(ctx, "WithInputFormatContext is not implemented for client.Kernel")
}

func (k *Kernel) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return kernel.ErrNotImplemented{}
}
