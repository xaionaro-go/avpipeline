package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type PacketFilter struct {
	*closuresignaler.ClosureSignaler
	Condition packetorframecondition.Condition
}

var _ Abstract = (*PacketFilter)(nil)

func NewPacketFilter(
	condition packetorframecondition.Condition,
) *PacketFilter {
	return &PacketFilter{
		ClosureSignaler: closuresignaler.New(),
		Condition:       condition,
	}
}

func (f *PacketFilter) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if f.Condition != nil && !f.Condition.Match(ctx, input) {
		return nil
	}

	output := input.CloneAsReferencedOutput()
	if output.Get() == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	outputCh <- output
	return nil
}

func (f *PacketFilter) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *PacketFilter) String() string {
	return fmt.Sprintf("ConditionFilter(%s)", f.Condition)
}

func (f *PacketFilter) Close(ctx context.Context) error {
	f.ClosureSignaler.Close(ctx)
	return nil
}

func (f *PacketFilter) CloseChan() <-chan struct{} {
	return f.ClosureSignaler.CloseChan()
}

func (f *PacketFilter) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
