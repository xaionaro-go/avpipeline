// av_filter_graph.go implements a kernel that wraps FFmpeg filter graphs.

package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type AVFilterGraph struct {
	*closuresignaler.ClosureSignaler
	avfilter.AVFilter[avfilter.Kernel]
}

var _ Abstract = (*AVFilterGraph)(nil)

// NewAVFilterGraph creates a new AVFilterGraph from an avfilter.Kernel.
func NewAVFilterGraph(
	ctx context.Context,
	k avfilter.Kernel,
) *AVFilterGraph {
	return &AVFilterGraph{
		ClosureSignaler: closuresignaler.New(),
		AVFilter: avfilter.AVFilter[avfilter.Kernel]{
			Kernel: k,
		},
	}
}

func (f *AVFilterGraph) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(f)
}

func (f *AVFilterGraph) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	if f.Condition != nil && !f.Condition.Match(ctx, *frameInput) {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	streamIdx := input.GetStreamIndex()
	if err := f.Kernel.AddFrame(streamIdx, frameInput.Frame, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return fmt.Errorf("unable to add frame to filter source for stream %d: %w", streamIdx, err)
	}

	for _, outStreamIdx := range f.Kernel.GetOutputStreams() {
		for {
			outFrame := frame.Pool.Get()
			if err := f.Kernel.GetFrame(outStreamIdx, outFrame, astiav.NewBuffersinkFlags()); err != nil {
				frame.Pool.Put(outFrame)
				if err == astiav.ErrEof || err == astiav.ErrEagain {
					break
				}
				return fmt.Errorf("unable to get frame from filter sink for stream %d: %w", outStreamIdx, err)
			}

			// TODO: get proper stream info if it changed
			outputFrame := frame.BuildOutput(outFrame, frameInput.StreamInfo)
			outputFrame.StreamIndex = outStreamIdx
			select {
			case <-ctx.Done():
				return ctx.Err()
			case outputCh <- packetorframe.OutputUnion{Frame: &outputFrame}:
			}
		}
	}

	return nil
}

func (f *AVFilterGraph) String() string {
	return "AVFilterGraph"
}

func (f *AVFilterGraph) Close(ctx context.Context) error {
	f.ClosureSignaler.Close(ctx)
	if f.Kernel != nil {
		return f.Kernel.Close()
	}
	return nil
}

func (f *AVFilterGraph) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
