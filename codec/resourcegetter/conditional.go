package resourcegetter

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
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...types.EncoderFactoryOption,
) *Resources {
	if c.Condition == nil {
		logger.Tracef(ctx, "no condition set, so always matching")
		return c.ResourcesGetter.GetResources(ctx, params, timeBase, opts...)
	}
	if c.Condition.Match(ctx, Input{
		Params:   params,
		TimeBase: timeBase,
		Options:  opts,
	}) {
		return c.ResourcesGetter.GetResources(ctx, params, timeBase, opts...)
	}
	return nil
}
