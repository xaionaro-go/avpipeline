// input_factory.go implements the input factory for the fallback demo.
package main

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/preset/inputwithfallback"
	"github.com/xaionaro-go/secret"
)

type inputFactory struct {
	URL string
}

var _ inputwithfallback.InputFactory[*kernel.Input, codec.DecoderFactory, struct{}] = (*inputFactory)(nil)

func (f *inputFactory) String() string {
	return f.URL
}

func (f *inputFactory) NewInput(
	ctx context.Context,
	_ *inputwithfallback.InputChain[*kernel.Input, codec.DecoderFactory, struct{}],
) (*kernel.Input, error) {
	return kernel.NewInputFromURL(ctx, f.URL, secret.New(""), kernel.InputConfig{})
}

func (f *inputFactory) NewDecoderFactory(
	context.Context,
	*inputwithfallback.InputChain[*kernel.Input, codec.DecoderFactory, struct{}],
) (codec.DecoderFactory, error) {
	return nil, nil // no decoding
}
