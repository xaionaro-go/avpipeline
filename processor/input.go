package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/secret"
)

func NewInputFromURL(
	ctx context.Context,
	url string,
	authKey secret.String,
	cfg kernel.InputConfig,
	processorOpts ...Option,
) (Abstract, error) {
	k, err := kernel.NewInputFromURL(ctx, url, authKey, cfg)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append([]Option{
			OptionQueueSizeInput(0),
			OptionQueueSizeOutput(1),
			OptionQueueSizeError(2),
		}, processorOpts...)...,
	), nil
}
