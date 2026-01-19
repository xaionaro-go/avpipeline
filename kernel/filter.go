package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Filter struct {
	*closuresignaler.ClosureSignaler
	Condition packetorframecondition.Condition
}

var _ Abstract = (*Filter)(nil)

func NewFilter(
	condition packetorframecondition.Condition,
) *Filter {
	return &Filter{
		ClosureSignaler: closuresignaler.New(),
		Condition:       condition,
	}
}

func (f *Filter) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if f.Condition != nil && !f.Condition.Match(ctx, input) {
		return nil
	}
	outputCh <- input.CloneAsReferencedOutput()
	return nil
}

func (f *Filter) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *Filter) String() string {
	return fmt.Sprintf("Filter(%s)", f.Condition)
}

func (f *Filter) Close(ctx context.Context) error {
	f.ClosureSignaler.Close(ctx)
	return nil
}

func (f *Filter) CloseChan() <-chan struct{} {
	return f.ClosureSignaler.CloseChan()
}

func (f *Filter) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
