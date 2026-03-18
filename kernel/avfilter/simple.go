package avfilter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
)

// SimpleVideoGraphConfig describes a single-input single-output video filter graph.
type SimpleVideoGraphConfig struct {
	Width             int
	Height            int
	PixelFormat       astiav.PixelFormat
	TimeBase          astiav.Rational
	SampleAspectRatio astiav.Rational
}

// SimpleAudioGraphConfig describes a single-input single-output audio filter graph.
type SimpleAudioGraphConfig struct {
	SampleRate    int
	SampleFormat  astiav.SampleFormat
	ChannelLayout astiav.ChannelLayout
	TimeBase      astiav.Rational
}

// SimpleGraph is a single-input, single-output filter graph that applies
// an arbitrary FFmpeg filter string to frames. It handles all the
// buffersrc/buffersink boilerplate internally.
type SimpleGraph struct {
	fg      *astiav.FilterGraph
	srcCtx  *astiav.BuffersrcFilterContext
	sinkCtx *astiav.BuffersinkFilterContext
}

var _ FrameFilter = (*SimpleGraph)(nil)

// NewSimpleVideoGraph creates a single-stream video filter graph.
//
// Usage:
//
//	g, err := avfilter.NewSimpleVideoGraph(ctx, "transpose=1", avfilter.SimpleVideoGraphConfig{
//	    Width: 1920, Height: 1080,
//	    PixelFormat: astiav.PixelFormatYuv420P,
//	    TimeBase: astiav.NewRational(1, 30),
//	})
func NewSimpleVideoGraph(
	_ context.Context,
	filterStr string,
	cfg SimpleVideoGraphConfig,
) (*SimpleGraph, error) {
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
	params.SetWidth(cfg.Width)
	params.SetHeight(cfg.Height)
	params.SetPixelFormat(cfg.PixelFormat)
	params.SetTimeBase(cfg.TimeBase)
	params.SetSampleAspectRatio(cfg.SampleAspectRatio)

	if err := srcCtx.SetParameters(params); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to set buffersrc parameters: %w", err)
	}

	if err := srcCtx.Initialize(nil); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to initialize buffersrc: %w", err)
	}

	if err := parseAndConfigure(fg, filterStr, srcCtx, sinkCtx); err != nil {
		fg.Free()
		return nil, err
	}

	return &SimpleGraph{
		fg:      fg,
		srcCtx:  srcCtx,
		sinkCtx: sinkCtx,
	}, nil
}

// NewSimpleVideoGraphFromFrame creates a video filter graph using
// format parameters extracted from an existing frame.
func NewSimpleVideoGraphFromFrame(
	ctx context.Context,
	filterStr string,
	f *astiav.Frame,
	timeBase astiav.Rational,
) (*SimpleGraph, error) {
	return NewSimpleVideoGraph(ctx, filterStr, SimpleVideoGraphConfig{
		Width:             f.Width(),
		Height:            f.Height(),
		PixelFormat:       f.PixelFormat(),
		TimeBase:          timeBase,
		SampleAspectRatio: f.SampleAspectRatio(),
	})
}

// NewSimpleAudioGraph creates a single-stream audio filter graph.
func NewSimpleAudioGraph(
	_ context.Context,
	filterStr string,
	cfg SimpleAudioGraphConfig,
) (*SimpleGraph, error) {
	fg := astiav.AllocFilterGraph()
	if fg == nil {
		return nil, fmt.Errorf("unable to allocate filter graph")
	}

	srcFilter := astiav.FindFilterByName("abuffer")
	sinkFilter := astiav.FindFilterByName("abuffersink")
	if srcFilter == nil || sinkFilter == nil {
		fg.Free()
		return nil, fmt.Errorf("unable to find abuffer or abuffersink filters")
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
	params.SetSampleRate(cfg.SampleRate)
	params.SetSampleFormat(cfg.SampleFormat)
	params.SetChannelLayout(cfg.ChannelLayout)
	params.SetTimeBase(cfg.TimeBase)

	if err := srcCtx.SetParameters(params); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to set buffersrc parameters: %w", err)
	}

	if err := srcCtx.Initialize(nil); err != nil {
		fg.Free()
		return nil, fmt.Errorf("unable to initialize buffersrc: %w", err)
	}

	if err := parseAndConfigure(fg, filterStr, srcCtx, sinkCtx); err != nil {
		fg.Free()
		return nil, err
	}

	return &SimpleGraph{
		fg:      fg,
		srcCtx:  srcCtx,
		sinkCtx: sinkCtx,
	}, nil
}

// ProcessFrame sends a frame through the filter graph and returns all output frames.
// Returns zero frames if the filter is buffering, or multiple frames for filters
// that produce more outputs than inputs.
func (g *SimpleGraph) ProcessFrame(
	_ context.Context,
	in *astiav.Frame,
) ([]*astiav.Frame, error) {
	if err := g.srcCtx.AddFrame(in, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return nil, fmt.Errorf("unable to add frame to filter: %w", err)
	}

	var result []*astiav.Frame
	for {
		outFrame := frame.Pool.Get()
		if err := g.sinkCtx.GetFrame(outFrame, astiav.NewBuffersinkFlags()); err != nil {
			frame.Pool.Put(outFrame)
			if err == astiav.ErrEof || err == astiav.ErrEagain {
				break
			}
			// Return collected frames to the pool to avoid a leak.
			for _, f := range result {
				frame.Pool.Put(f)
			}
			return nil, fmt.Errorf("unable to get frame from filter: %w", err)
		}
		result = append(result, outFrame)
	}
	return result, nil
}

// AddFrame sends a frame into the filter graph without pulling output.
// The streamIdx parameter is ignored for single-stream graphs.
func (g *SimpleGraph) AddFrame(
	_ int,
	f *astiav.Frame,
	flags astiav.BuffersrcFlags,
) error {
	return g.srcCtx.AddFrame(f, flags)
}

// GetFrame pulls a single frame from the filter graph.
// The streamIdx parameter is ignored for single-stream graphs.
func (g *SimpleGraph) GetFrame(
	_ int,
	f *astiav.Frame,
	flags astiav.BuffersinkFlags,
) error {
	return g.sinkCtx.GetFrame(f, flags)
}

// GetOutputStreams returns the output stream indices. For SimpleGraph,
// this is always [0].
func (g *SimpleGraph) GetOutputStreams() []int {
	return []int{0}
}

// Close frees the filter graph and all associated resources.
func (g *SimpleGraph) Close() error {
	if g.fg != nil {
		g.fg.Free()
		g.fg = nil
	}
	return nil
}

func parseAndConfigure(
	fg *astiav.FilterGraph,
	filterStr string,
	srcCtx *astiav.BuffersrcFilterContext,
	sinkCtx *astiav.BuffersinkFilterContext,
) error {
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

	if err := fg.Parse(filterStr, inputs, outputs); err != nil {
		return fmt.Errorf("unable to parse filter string %q: %w", filterStr, err)
	}

	if err := fg.Configure(); err != nil {
		return fmt.Errorf("unable to configure filter graph: %w", err)
	}

	return nil
}
