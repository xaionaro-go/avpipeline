// dummy_test.go contains tests for the dummy kernel.

package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Dummy struct {
	SendInputFn func(
		ctx context.Context,
		input packetorframe.InputUnion,
		outputCh chan<- packetorframe.OutputUnion,
	) error
	SendInputCallCount int

	SendPacketCallCount int
	SendFrameCallCount  int

	CloseFn        func(context.Context) error
	CloseCallCount int

	CloseChanFn        func() <-chan struct{}
	CloseChanCallCount int

	GenerateFn func(
		ctx context.Context,
		outputCh chan<- packetorframe.OutputUnion,
	) error
	GenerateCallCount int
}

var _ Abstract = (*Dummy)(nil)

func (d *Dummy) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	d.SendInputCallCount++
	switch {
	case input.Packet != nil:
		d.SendPacketCallCount++
	case input.Frame != nil:
		d.SendFrameCallCount++
	default:
		return types.ErrUnexpectedInputType{}
	}
	if d.SendInputFn == nil {
		return nil
	}
	return d.SendInputFn(ctx, input, outputCh)
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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	d.GenerateCallCount++
	if d.GenerateFn == nil {
		return nil
	}
	return d.GenerateFn(ctx, outputCh)
}
