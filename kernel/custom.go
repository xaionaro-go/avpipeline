package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Custom struct {
	*closeChan
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

var _ Abstract = (*Custom)(nil)

func NewCustom(
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
) *Custom {
	m := &Custom{
		closeChan:           newCloseChan(),
		GenerateFunc:        generate,
		SendInputPacketFunc: sendInputPacket,
		SendInputFrameFunc:  sendInputFrame,
		CloseFunc:           close,
	}
	return m
}

func (m *Custom) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFrameCh chan<- frame.Output,
) error {
	if m.SendInputPacketFunc == nil {
		return nil
	}
	return m.SendInputPacketFunc(ctx, input, outputPacketsCh, outputFrameCh)
}

func (m *Custom) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if m.SendInputFrameFunc == nil {
		return nil
	}
	return m.SendInputFrameFunc(ctx, input, outputPacketsCh, outputFramesCh)
}

func (m *Custom) String() string {
	return "Custom"
}

func (m *Custom) Close(ctx context.Context) error {
	m.closeChan.Close(ctx)
	if m.CloseFunc == nil {
		return nil
	}
	return m.CloseFunc(ctx)
}

func (m *Custom) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if m.GenerateFunc == nil {
		return nil
	}
	return m.GenerateFunc(ctx, outputPacketsCh, outputFramesCh)
}
