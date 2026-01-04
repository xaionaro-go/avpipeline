// buffer.go implements a buffer filter (src and sink).

package avfilter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Buffer struct {
	Src     *astiav.Filter
	Sink    *astiav.Filter
	SrcCtx  *astiav.BuffersrcFilterContext
	SinkCtx *astiav.BuffersinkFilterContext
}

var _ Kernel = (*Buffer)(nil)

func NewBuffer(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*AVFilter[*Buffer], error) {
	b := &Buffer{}

	buffersrcContextParameters := astiav.AllocBuffersrcFilterContextParameters()
	setFinalizerFree(ctx, buffersrcContextParameters)

	var content string
	switch codecParams.MediaType() {
	case astiav.MediaTypeAudio:
		b.Src = astiav.FindFilterByName("abuffer")
		buffersrcContextParameters.SetChannelLayout(codecParams.ChannelLayout())
		buffersrcContextParameters.SetSampleFormat(codecParams.SampleFormat())
		buffersrcContextParameters.SetSampleRate(codecParams.SampleRate())
		buffersrcContextParameters.SetTimeBase(timeBase)
		b.Sink = astiav.FindFilterByName("abuffersink")
		content = fmt.Sprintf(
			"aformat=sample_fmts=%s:channel_layouts=%s",
			codecParams.SampleFormat().Name(), codecParams.ChannelLayout().String(),
		)
	default:
		b.Src = astiav.FindFilterByName("buffer")
		buffersrcContextParameters.SetHeight(codecParams.Height())
		buffersrcContextParameters.SetPixelFormat(codecParams.PixelFormat())
		buffersrcContextParameters.SetSampleAspectRatio(codecParams.SampleAspectRatio())
		buffersrcContextParameters.SetTimeBase(timeBase)
		buffersrcContextParameters.SetWidth(codecParams.Width())
		b.Sink = astiav.FindFilterByName("buffersink")
		content = fmt.Sprintf(
			"format=pix_fmts=%s",
			codecParams.PixelFormat().Name(),
		)
	}

	if b.Src == nil {
		return nil, fmt.Errorf("unable to find the filter by name")
	}
	if b.Sink == nil {
		return nil, fmt.Errorf("unable to find the filter sink by name")
	}

	return &AVFilter[*Buffer]{
		Kernel:  b,
		Content: content,
	}, nil
}

func (b *Buffer) FilterInput() *astiav.Filter {
	return b.Sink
}

func (b *Buffer) FilterOutput() *astiav.Filter {
	return b.Src
}

func (b *Buffer) ConnectInput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	var err error
	b.SrcCtx, err = graph.NewBuffersrcFilterContext(b.Src, nodeName)
	return err
}

func (b *Buffer) ConnectOutput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	var err error
	b.SinkCtx, err = graph.NewBuffersinkFilterContext(b.Sink, nodeName)
	return err
}

func (b *Buffer) InputFilterContext() *astiav.FilterContext {
	return b.SrcCtx.FilterContext()
}

func (b *Buffer) OutputFilterContext() *astiav.FilterContext {
	return b.SinkCtx.FilterContext()
}
