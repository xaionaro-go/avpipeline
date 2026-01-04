// boxblur.go implements a boxblur filter.

package avfilter

import (
	"context"
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
)

type BoxBlur struct {
	AVFilter *astiav.Filter
}

var _ Kernel = (*BoxBlur)(nil)

type BoxBlurParams struct {
	LumaRadius   *uint
	LumaPower    *uint
	ChromaRadius *uint
	ChromaPower  *uint
	AlphaRadius  *uint
	AlphaPower   *uint
	Width        *uint
	Height       *uint
	ChromaWidth  *uint
	ChromaHeight *uint
	HSubSampling *uint
	VSubSampling *uint
}

func NewBoxBlur(
	ctx context.Context,
	codecParams *astiav.CodecParameters,
	params BoxBlurParams,
) (*AVFilter[*BoxBlur], error) {
	b := &BoxBlur{}

	var content string
	switch codecParams.MediaType() {
	case astiav.MediaTypeVideo:
		b.AVFilter = astiav.FindFilterByName("boxblur")
		if b.AVFilter == nil {
			return nil, fmt.Errorf("unable to find the filter by name")
		}
		var values []string
		if params.LumaRadius != nil {
			values = append(values, fmt.Sprintf("luma_radius=%d", *params.LumaRadius))
		}
		if params.LumaPower != nil {
			values = append(values, fmt.Sprintf("luma_power=%d", *params.LumaPower))
		}
		if params.ChromaRadius != nil {
			values = append(values, fmt.Sprintf("chroma_radius=%d", *params.ChromaRadius))
		}
		if params.ChromaPower != nil {
			values = append(values, fmt.Sprintf("chroma_power=%d", *params.ChromaPower))
		}
		if params.ChromaPower != nil {
			values = append(values, fmt.Sprintf("alpha_power=%d", *params.AlphaPower))
		}
		if params.AlphaPower != nil {
			values = append(values, fmt.Sprintf("alpha_power=%d", *params.AlphaPower))
		}
		if params.Width != nil {
			values = append(values, fmt.Sprintf("w=%d", *params.Width))
		}
		if params.Height != nil {
			values = append(values, fmt.Sprintf("h=%d", *params.Height))
		}
		if params.ChromaWidth != nil {
			values = append(values, fmt.Sprintf("cw=%d", *params.ChromaWidth))
		}
		if params.ChromaHeight != nil {
			values = append(values, fmt.Sprintf("ch=%d", *params.ChromaHeight))
		}
		if params.HSubSampling != nil {
			values = append(values, fmt.Sprintf("hsub=%d", *params.HSubSampling))
		}
		if params.VSubSampling != nil {
			values = append(values, fmt.Sprintf("vsub=%d", *params.HSubSampling))
		}
		content = strings.Join(values, ":")
	}

	return &AVFilter[*BoxBlur]{
		Kernel:  b,
		Content: content,
	}, nil
}

func (b *BoxBlur) FilterInput() *astiav.Filter {
	return b.AVFilter
}

func (b *BoxBlur) FilterOutput() *astiav.Filter {
	return b.AVFilter
}

func (b *BoxBlur) ConnectInput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	return fmt.Errorf("not implemented")
}

func (b *BoxBlur) ConnectOutput(
	graph *astiav.FilterGraph,
	nodeName string,
) error {
	return fmt.Errorf("not implemented")
}

func (b *BoxBlur) InputFilterContext() *astiav.FilterContext {
	panic("not implemented, yet")
}

func (b *BoxBlur) OutputFilterContext() *astiav.FilterContext {
	panic("not implemented, yet")
}
