package client

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Kernel struct{}

var _ kernel.Abstract = (*Kernel)(nil)
var _ packet.Source = (*Kernel)(nil)
var _ packet.Sink = (*Kernel)(nil)

func (k *Kernel) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	panic("not implemented")
}

func (k *Kernel) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
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
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
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
