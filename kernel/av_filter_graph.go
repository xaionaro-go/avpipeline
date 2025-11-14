package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type AVFilterGraph[T avfilter.Kernel] struct {
	*closuresignaler.ClosureSignaler
	*astiav.FilterGraph
	Inputs  *astiav.FilterInOut
	Outputs *astiav.FilterInOut
	Filters []avfilter.AVFilter[T]
}

var _ Abstract = (*AVFilterGraph[avfilter.Kernel])(nil)

// experimental: API will change in the future
func NewAVFilterGraph[T avfilter.Kernel](
	ctx context.Context,
	filters ...avfilter.AVFilter[T],
) (*AVFilterGraph[T], error) {
	f := &AVFilterGraph[T]{
		ClosureSignaler: closuresignaler.New(),
		FilterGraph:     astiav.AllocFilterGraph(),
		Inputs:          astiav.AllocFilterInOut(),
		Outputs:         astiav.AllocFilterInOut(),
	}
	setFinalizerFree(ctx, f.FilterGraph)
	setFinalizerFree(ctx, f.Inputs)
	setFinalizerFree(ctx, f.Outputs)
	if f.FilterGraph == nil {
		return nil, fmt.Errorf("unable to allocate FilterGraph")
	}
	if f.Inputs == nil {
		return nil, fmt.Errorf("unable to allocate Inputs")
	}
	if f.Outputs == nil {
		return nil, fmt.Errorf("unable to allocate Outputs")
	}

	for _, filter := range filters {
		filter.Kernel.ConnectInput(f.FilterGraph, "in")
		filter.Kernel.ConnectOutput(f.FilterGraph, "out")

		f.Inputs.SetName("out") // should it be "in"
		f.Inputs.SetFilterContext(filter.OutputFilterContext())
		f.Inputs.SetPadIdx(0)
		f.Inputs.SetNext(nil)

		f.Outputs.SetName("in")
		f.Outputs.SetFilterContext(filter.InputFilterContext())
		f.Outputs.SetPadIdx(0)
		f.Outputs.SetNext(nil)

		if err := f.FilterGraph.Parse(filter.Content, f.Inputs, f.Outputs); err != nil {
			return nil, fmt.Errorf("unable to parse the filter graph: %w", err)
		}
	}

	if err := f.FilterGraph.Configure(); err != nil {
		return nil, fmt.Errorf("unable to configure the filter graph: %w", err)
	}

	f.Filters = filters
	return f, nil
}

func (f *AVFilterGraph[T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *AVFilterGraph[T]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("a Filter is supposed to be used only for Frame-s, not Packet-s")
}

func (f *AVFilterGraph[T]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	for _, filter := range f.Filters {
		if filter.Condition.Match(ctx, input) {
		}
	}
	return nil
}

func (f *AVFilterGraph[T]) String() string {
	return "Filter"
}

func (f *AVFilterGraph[T]) Close(ctx context.Context) error {
	f.ClosureSignaler.Close(ctx)
	return nil
}

func (f *AVFilterGraph[T]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return nil
}
