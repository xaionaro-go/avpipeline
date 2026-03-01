package avfilter

import (
	"context"
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
)

type TrackConfig struct {
	Filters         []string
	CodecParameters *astiav.CodecParameters
	TimeBase        astiav.Rational
	NoOutput        bool
}

type Graph struct {
	FilterGraph *astiav.FilterGraph
	Inputs      map[int]*Buffer
	Outputs     map[int]*Buffer
}

func NewGraph(
	ctx context.Context,
	config map[int]TrackConfig,
	filterComplex string,
) (*Graph, error) {
	g := &Graph{
		FilterGraph: astiav.AllocFilterGraph(),
		Inputs:      make(map[int]*Buffer),
		Outputs:     make(map[int]*Buffer),
	}
	setFinalizerFree(ctx, g.FilterGraph)

	if g.FilterGraph == nil {
		return nil, fmt.Errorf("unable to allocate FilterGraph")
	}

	useComplex := filterComplex != ""
	var inputs, outputs []*astiav.FilterInOut

	for streamIdx, trackCfg := range config {
		bufFilter, err := NewBuffer(ctx, trackCfg.CodecParameters, trackCfg.TimeBase)
		if err != nil {
			return nil, fmt.Errorf("unable to create buffer for stream %d: %w", streamIdx, err)
		}
		buf := bufFilter.Kernel

		if err := buf.ConnectInput(g.FilterGraph, fmt.Sprintf("src%d", streamIdx)); err != nil {
			return nil, fmt.Errorf("unable to connect input for stream %d: %w", streamIdx, err)
		}

		inName := fmt.Sprintf("in%d", streamIdx)
		in := astiav.AllocFilterInOut()
		in.SetName(inName)
		in.SetFilterContext(buf.InputFilterContext())
		in.SetPadIdx(0)
		if useComplex {
			inputs = append(inputs, in)
		}

		g.Inputs[streamIdx] = buf

		if !trackCfg.NoOutput {
			if err := buf.ConnectOutput(g.FilterGraph, fmt.Sprintf("sink%d", streamIdx)); err != nil {
				return nil, fmt.Errorf("unable to connect output for stream %d: %w", streamIdx, err)
			}

			outName := fmt.Sprintf("out%d", streamIdx)
			out := astiav.AllocFilterInOut()
			out.SetName(outName)
			out.SetFilterContext(buf.OutputFilterContext())
			out.SetPadIdx(0)
			if useComplex {
				outputs = append(outputs, out)
			}

			g.Outputs[streamIdx] = buf

			filterStr := strings.Join(trackCfg.Filters, ",")
			if filterStr != "" {
				if err := g.FilterGraph.Parse(filterStr, out, in); err != nil {
					in.Free()
					out.Free()
					return nil, fmt.Errorf("unable to parse filters for stream %d: %w", streamIdx, err)
				}
				if !useComplex {
					in.Free()
					out.Free()
				}
			} else if !useComplex {
				in.Free()
				out.Free()
			}
		} else if !useComplex {
			in.Free()
		}
	}

	if useComplex {
		for i := 0; i < len(inputs)-1; i++ {
			inputs[i].SetNext(inputs[i+1])
		}
		for i := 0; i < len(outputs)-1; i++ {
			outputs[i].SetNext(outputs[i+1])
		}

		var firstInput *astiav.FilterInOut
		if len(inputs) > 0 {
			firstInput = inputs[0]
		}
		var firstOutput *astiav.FilterInOut
		if len(outputs) > 0 {
			firstOutput = outputs[0]
		}

		if err := g.FilterGraph.Parse(filterComplex, firstOutput, firstInput); err != nil {
			if firstInput != nil {
				firstInput.Free()
			}
			if firstOutput != nil {
				firstOutput.Free()
			}
			return nil, fmt.Errorf("unable to parse filter_complex: %w", err)
		}
		if firstInput != nil {
			firstInput.Free()
		}
		if firstOutput != nil {
			firstOutput.Free()
		}
	}

	if err := g.FilterGraph.Configure(); err != nil {
		return nil, fmt.Errorf("unable to configure the filter graph: %w", err)
	}

	return g, nil
}

var _ FrameFilter = (*Graph)(nil)

func (g *Graph) AddFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersrcFlags) error {
	buf, ok := g.Inputs[streamIdx]
	if !ok {
		return fmt.Errorf("stream %d not found in graph inputs", streamIdx)
	}
	return buf.AddFrame(0, f, flags)
}

func (g *Graph) GetFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersinkFlags) error {
	buf, ok := g.Outputs[streamIdx]
	if !ok {
		return fmt.Errorf("stream %d not found in graph outputs", streamIdx)
	}
	return buf.GetFrame(0, f, flags)
}

func (g *Graph) GetOutputStreams() []int {
	var streams []int
	for streamIdx := range g.Outputs {
		streams = append(streams, streamIdx)
	}
	return streams
}

func (g *Graph) Close() error {
	if g.FilterGraph != nil {
		g.FilterGraph.Free()
		g.FilterGraph = nil
	}
	return nil
}
