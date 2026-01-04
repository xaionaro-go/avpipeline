// kernel.go implements a client-side kernel.

// Package client provides a client-side kernel implementation.
package client

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
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
	panic("not implemented")
}

func (k *Kernel) String() string {
	panic("not implemented")
}

func (k *Kernel) Close(ctx context.Context) error {
	panic("not implemented")
}

func (k *Kernel) CloseChan() <-chan struct{} {
	panic("not implemented")
}

func (k *Kernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	panic("not implemented")
}

func (k *Kernel) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	panic("not implemented")
}

func (k *Kernel) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	panic("not implemented")
}

func (k *Kernel) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	panic("not implemented")
}
