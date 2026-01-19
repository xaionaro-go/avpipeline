package avfilter

import (
	"context"
	"fmt"
	"math"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
)

type FrameRotator struct {
	fg      *astiav.FilterGraph
	srcCtx  *astiav.BuffersrcFilterContext
	sinkCtx *astiav.BuffersinkFilterContext
}

func NewFrameRotator(
	ctx context.Context,
	f *astiav.Frame,
	timeBase astiav.Rational,
	rotation float64,
) (*FrameRotator, error) {
	fg := astiav.AllocFilterGraph()
	if fg == nil {
		return nil, fmt.Errorf("unable to allocate filter graph")
	}

	srcFilter := astiav.FindFilterByName("buffer")
	sinkFilter := astiav.FindFilterByName("buffersink")
	if srcFilter == nil || sinkFilter == nil {
		fg.Free()
		return nil, fmt.Errorf("unable to find buffer or buffersink filters")
	}

	srcCtx, err := fg.NewBuffersrcFilterContext(srcFilter, "in")
	if err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to create buffersrc context: %w", err)
	}

	sinkCtx, err := fg.NewBuffersinkFilterContext(sinkFilter, "out")
	if err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to create buffersink context: %w", err)
	}

	params := astiav.AllocBuffersrcFilterContextParameters()
	defer params.Free()
	params.SetWidth(f.Width())
	params.SetHeight(f.Height())
	params.SetPixelFormat(f.PixelFormat())
	params.SetTimeBase(timeBase)
	params.SetSampleAspectRatio(f.SampleAspectRatio())

	if err := srcCtx.SetParameters(params); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to set buffersrc parameters: %w", err)
	}

	if err := srcCtx.Initialize(nil); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to initialize buffersrc: %w", err)
	}

	outputs := astiav.AllocFilterInOut()
	defer outputs.Free()
	outputs.SetName("in")
	outputs.SetFilterContext(srcCtx.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	inputs := astiav.AllocFilterInOut()
	defer inputs.Free()
	inputs.SetName("out")
	inputs.SetFilterContext(sinkCtx.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	var filterContent string
	normRotation := math.Mod(rotation, 360)
	if normRotation < 0 {
		normRotation += 360
	}
	switch normRotation {
	case 90:
		filterContent = "transpose=1"
	case 180:
		filterContent = "transpose=1,transpose=1"
	case 270:
		filterContent = "transpose=2"
	case 0:
		fg.Free()
		return nil, fmt.Errorf("no rotation needed")
	default:
		fg.Free()
		return nil, fmt.Errorf("unsupported rotation angle: %f", rotation)
	}

	if err := fg.Parse(filterContent, inputs, outputs); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to parse filter string %q: %w", filterContent, err)
	}

	if err := fg.Configure(); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to configure filter graph: %w", err)
	}

	return &FrameRotator{
		fg:      fg,
		srcCtx:  srcCtx,
		sinkCtx: sinkCtx,
	}, nil
}

func (fr *FrameRotator) Rotate(ctx context.Context, in *astiav.Frame) (*astiav.Frame, error) {
	if err := fr.srcCtx.AddFrame(in, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return nil, fmt.Errorf("unable to add frame to buffersrc: %w", err)
	}
	out := frame.Pool.Get()
	if err := fr.sinkCtx.GetFrame(out, astiav.BuffersinkFlags(0)); err != nil {
		frame.Pool.Put(out)
		return nil, fmt.Errorf("unable to get frame from buffersink: %w", err)
	}
	return out, nil
}

func (fr *FrameRotator) Close() {
	if fr == nil {
		return
	}
	if fr.fg != nil {
		fr.fg.Free()
		fr.fg = nil
	}
}
