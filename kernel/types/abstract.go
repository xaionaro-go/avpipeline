package types

import (
	"github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
)

type Abstract interface {
	typesnolibav.Abstract
	SendInputer
	Generator
}

/*

== for easier copy&paste ==

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func (k *MyFancyAbstractPlaceholder) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *MyFancyAbstractPlaceholder) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
}

func (k *MyFancyAbstractPlaceholder) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
}

func (k *MyFancyAbstractPlaceholder) String() string {
	return "MyFancyAbstractPlaceholder"
}

func (k *MyFancyAbstractPlaceholder) Close(ctx context.Context) (_err error) {
}

func (k *MyFancyAbstractPlaceholder) CloseChan() <-chan struct{} {
}

func (k *MyFancyAbstractPlaceholder) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
}

== for a packet source also: ==

func (k *MyFancyAbstractPlaceholder) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {

}

== for a packet sink also: ==

func (k *MyFancyAbstractPlaceholder) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {

}

func (k *MyFancyAbstractPlaceholder) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_err error) {

}

*/
