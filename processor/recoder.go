package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
)

var DefaultOptionsRecoder = func() []Option {
	return []Option{
		OptionQueueSizeInput(600),
		OptionQueueSizeOutput(0),
		OptionQueueSizeError(2),
	}
}

func NewRecoder(
	ctx context.Context,
	decoderFactory codec.DecoderFactory,
	encoderFactory codec.EncoderFactory,
	streamConfigurer kernel.StreamConfigurer,
	processorOpts ...Option,
) (Abstract, error) {
	k, err := kernel.NewRecoder(
		ctx,
		decoderFactory,
		encoderFactory,
		streamConfigurer,
	)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append(DefaultOptionsRecoder(), processorOpts...)...,
	), nil
}
