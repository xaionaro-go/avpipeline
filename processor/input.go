// input.go provides factory functions to instantiate FromKernel processors for input sources.

package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/secret"
)

var DefaultOptionsInput = func() []Option {
	return []Option{
		OptionQueueSizeInput(0),
		OptionQueueSizeOutput(1),
		OptionQueueSizeError(2),
	}
}

func NewInputFromURL(
	ctx context.Context,
	url string,
	authKey secret.String,
	cfg kernel.InputConfig,
	processorOpts ...Option,
) (*FromKernel[*kernel.Input], error) {
	k, err := kernel.NewInputFromURL(ctx, url, authKey, cfg)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append(DefaultOptionsInput(), processorOpts...)...,
	), nil
}
