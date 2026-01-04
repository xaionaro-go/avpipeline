// conditional.go implements a conditional resource getter.

package resource

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
)

type Conditional struct {
	ResourcesGetter ResourcesGetter
	Condition       Condition
}

var _ ResourcesGetter = (*Conditional)(nil)

func NewConditional(
	rg ResourcesGetter,
	cond Condition,
) *Conditional {
	return &Conditional{
		ResourcesGetter: rg,
		Condition:       cond,
	}
}

func (c *Conditional) String() string {
	return "Conditional(" + c.ResourcesGetter.String() + ")"
}

func (c *Conditional) GetResources(
	ctx context.Context,
	isEncoder bool,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...types.Option,
) *Resources {
	if c.Condition == nil {
		logger.Tracef(ctx, "no condition set, so always matching")
		return c.ResourcesGetter.GetResources(ctx, isEncoder, params, timeBase)
	}
	if c.Condition.Match(ctx, GetterInput{
		Params:   params,
		TimeBase: timeBase,
	}) {
		return c.ResourcesGetter.GetResources(ctx, isEncoder, params, timeBase)
	}
	return nil
}
