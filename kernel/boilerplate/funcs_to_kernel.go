// funcs_to_kernel.go provides a wrapper to convert functions into a kernel.

package boilerplate

import (
	"context"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type FuncsToKernel struct {
	*closuresignaler.ClosureSignaler
	GenerateFunc func(
		ctx context.Context,
		outputCh chan<- packetorframe.OutputUnion,
	) error
	SendInputFunc func(
		ctx context.Context,
		input packetorframe.InputUnion,
		outputCh chan<- packetorframe.OutputUnion,
	) error
	CloseFunc func(context.Context) error
}

var _ types.Abstract = (*FuncsToKernel)(nil)

func NewFuncsToKernel(
	ctx context.Context,
	generate func(
		ctx context.Context,
		outputCh chan<- packetorframe.OutputUnion,
	) error,
	sendInput func(
		ctx context.Context,
		input packetorframe.InputUnion,
		outputCh chan<- packetorframe.OutputUnion,
	) error,
	close func(context.Context) error,
) *FuncsToKernel {
	f := &FuncsToKernel{
		ClosureSignaler: closuresignaler.New(),
		GenerateFunc:    generate,
		SendInputFunc:   sendInput,
		CloseFunc:       close,
	}
	return f
}

func (f *FuncsToKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if f.SendInputFunc == nil {
		return nil
	}
	return f.SendInputFunc(ctx, input, outputCh)
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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if f.GenerateFunc == nil {
		return nil
	}
	return f.GenerateFunc(ctx, outputCh)
}
