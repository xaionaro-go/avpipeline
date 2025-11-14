package boilerplate

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type FuncsToKernel struct {
	*closuresignaler.ClosureSignaler
	GenerateFunc func(
		ctx context.Context,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
	SendInputPacketFunc func(
		ctx context.Context,
		input packet.Input,
		outputPacketsCh chan<- packet.Output,
		_ chan<- frame.Output,
	) error
	SendInputFrameFunc func(
		ctx context.Context,
		input frame.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
	CloseFunc func(context.Context) error
}

var _ types.Abstract = (*FuncsToKernel)(nil)

func NewFuncsToKernel(
	ctx context.Context,
	generate func(
		ctx context.Context,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error,
	sendInputPacket func(
		ctx context.Context,
		input packet.Input,
		outputPacketsCh chan<- packet.Output,
		_ chan<- frame.Output,
	) error,
	sendInputFrame func(
		ctx context.Context,
		input frame.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error,
	close func(context.Context) error,
) *FuncsToKernel {
	f := &FuncsToKernel{
		ClosureSignaler:     closuresignaler.New(),
		GenerateFunc:        generate,
		SendInputPacketFunc: sendInputPacket,
		SendInputFrameFunc:  sendInputFrame,
		CloseFunc:           close,
	}
	return f
}

func (f *FuncsToKernel) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFrameCh chan<- frame.Output,
) error {
	if f.SendInputPacketFunc == nil {
		return nil
	}
	return f.SendInputPacketFunc(ctx, input, outputPacketsCh, outputFrameCh)
}

func (f *FuncsToKernel) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if f.SendInputFrameFunc == nil {
		return nil
	}
	return f.SendInputFrameFunc(ctx, input, outputPacketsCh, outputFramesCh)
}

func (f *FuncsToKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *FuncsToKernel) String() string {
	return "Custom"
}

func (f *FuncsToKernel) Close(ctx context.Context) error {
	f.ClosureSignaler.Close(ctx)
	if f.CloseFunc == nil {
		return nil
	}
	return f.CloseFunc(ctx)
}

func (f *FuncsToKernel) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if f.GenerateFunc == nil {
		return nil
	}
	return f.GenerateFunc(ctx, outputPacketsCh, outputFramesCh)
}
