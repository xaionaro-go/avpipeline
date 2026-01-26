package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type AVFilter struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Filters map[int]*AVFilterGraph
}

var _ Abstract = (*AVFilter)(nil)

func NewAVFilter(
	ctx context.Context,
	filters map[int]*AVFilterGraph,
) *AVFilter {
	return &AVFilter{
		ClosureSignaler: closuresignaler.New(),
		Filters:         filters,
	}
}

func (f *AVFilter) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return xsync.DoA3R1(ctx, &f.Locker, f.sendInput, ctx, input, outputCh)
}

func (f *AVFilter) sendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	filter, ok := f.Filters[input.GetStreamIndex()]
	if !ok {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	return filter.SendInput(ctx, input, outputCh)
}

func (f *AVFilter) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *AVFilter) String() string {
	return "AVFilter"
}

func (f *AVFilter) Close(ctx context.Context) error {
	f.Locker.Do(ctx, func() {
		for _, filter := range f.Filters {
			filter.Close(ctx)
		}
		f.Filters = nil
	})
	f.ClosureSignaler.Close(ctx)
	return nil
}

func (f *AVFilter) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
