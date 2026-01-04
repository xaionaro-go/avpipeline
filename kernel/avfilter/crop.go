// crop.go implements a crop filter.

package avfilter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Crop struct {
	Src     *astiav.Filter
	Sink    *astiav.Filter
	SrcCtx  *astiav.BuffersrcFilterContext
	SinkCtx *astiav.BuffersinkFilterContext
}

var _ Kernel = (*Crop)(nil)

func NewCrop(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
	w, h, x, y int,
) (*AVFilter[*Crop], error) {
	b := &Crop{}

	BuffersrcContextParameters := astiav.AllocBuffersrcFilterContextParameters()
	setFinalizerFree(ctx, BuffersrcContextParameters)

	var content string
	switch codecParams.MediaType() {
	case astiav.MediaTypeVideo:
		b.Src = astiav.FindFilterByName("crop")
		BuffersrcContextParameters.SetChannelLayout(codecParams.ChannelLayout())
		BuffersrcContextParameters.SetSampleFormat(codecParams.SampleFormat())
		BuffersrcContextParameters.SetSampleRate(codecParams.SampleRate())
		BuffersrcContextParameters.SetTimeBase(timeBase)
		b.Sink = astiav.FindFilterByName("buffersink")
		content = fmt.Sprintf(
			"%d:%d:%d:%d",
			w, h, x, y,
		)
		if b.Src == nil {
			return nil, fmt.Errorf("unable to find the filter by name")
		}
		if b.Sink == nil {
			return nil, fmt.Errorf("unable to find the filter sink by name")
		}
	}

	return &AVFilter[*Crop]{
		Kernel:  b,
		Content: content,
	}, nil
}

func (b *Crop) FilterInput() *astiav.Filter {
	return b.Sink
}

func (b *Crop) FilterOutput() *astiav.Filter {
	return b.Src
}

func (b *Crop) ConnectInput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	var err error
	b.SrcCtx, err = graph.NewBuffersrcFilterContext(b.Src, nodeName)
	return err
}

func (b *Crop) ConnectOutput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	var err error
	b.SinkCtx, err = graph.NewBuffersinkFilterContext(b.Sink, nodeName)
	return err
}

func (b *Crop) InputFilterContext() *astiav.FilterContext {
	return b.SrcCtx.FilterContext()
}

func (b *Crop) OutputFilterContext() *astiav.FilterContext {
	return b.SinkCtx.FilterContext()
}
