package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Dummy struct {
	SendInputPacketFn func(
		ctx context.Context,
		input packet.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
	SendInputPacketCallCount int

	SendInputFrameFn func(
		ctx context.Context,
		input frame.Input,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
	SendInputFrameCallCount int

	CloseFn        func(context.Context) error
	CloseCallCount int

	CloseChanFn        func() <-chan struct{}
	CloseChanCallCount int

	GenerateFn func(
		ctx context.Context,
		outputPacketsCh chan<- packet.Output,
		outputFramesCh chan<- frame.Output,
	) error
	GenerateCallCount int
}

var _ Abstract = (*Dummy)(nil)

func (d *Dummy) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	d.SendInputPacketCallCount++
	if d.SendInputPacketFn == nil {
		return nil
	}
	return d.SendInputPacketFn(ctx, input, outputPacketsCh, outputFramesCh)
}

func (d *Dummy) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	d.SendInputFrameCallCount++
	if d.SendInputFrameFn == nil {
		return nil
	}
	return d.SendInputFrameFn(ctx, input, outputPacketsCh, outputFramesCh)
}

func (d *Dummy) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(d)
}

func (d *Dummy) String() string {
	return "Dummy"
}

func (d *Dummy) Close(ctx context.Context) error {
	d.CloseCallCount++
	if d.CloseFn == nil {
		return nil
	}
	return d.CloseFn(ctx)
}

func (d *Dummy) CloseChan() <-chan struct{} {
	d.CloseCallCount++
	if d.CloseChanFn == nil {
		return nil
	}
	return d.CloseChanFn()
}

func (d *Dummy) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	d.GenerateCallCount++
	if d.GenerateFn == nil {
		return nil
	}
	return d.GenerateFn(ctx, outputPacketsCh, outputFramesCh)
}
