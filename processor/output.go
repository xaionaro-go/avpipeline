package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/secret"
)

var DefaultOptionsOutput = func() []Option {
	return []Option{
		OptionQueueSizeInput(600),
		OptionQueueSizeOutput(0),
		OptionQueueSizeError(2),
	}
}

func NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey secret.String,
	cfg kernel.OutputConfig,
	processorOpts ...Option,
) (*FromKernel[*kernel.Output], error) {
	k, err := kernel.NewOutputFromURL(ctx, urlString, streamKey, cfg)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append(DefaultOptionsOutput(), processorOpts...)...,
	), nil
}
