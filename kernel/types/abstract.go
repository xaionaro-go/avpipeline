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

	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func (k *MyFancyAbstractPlaceholder) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *MyFancyAbstractPlaceholder) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	return nil
}

func (k *MyFancyAbstractPlaceholder) String() string {
	return "MyFancyAbstractPlaceholder"
}

func (k *MyFancyAbstractPlaceholder) Close(ctx context.Context) (_err error) {
	return nil
}

func (k *MyFancyAbstractPlaceholder) CloseChan() <-chan struct{} {
	return nil
}

func (k *MyFancyAbstractPlaceholder) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	return nil
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
*/
