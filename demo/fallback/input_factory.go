package main

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/preset/inputwithfallback"
	"github.com/xaionaro-go/avpipeline/urltools"
	"github.com/xaionaro-go/secret"
)

type inputFactory struct {
	URL string
}

var _ inputwithfallback.InputFactory[*kernel.Input, codec.DecoderFactory] = (*inputFactory)(nil)

func (f *inputFactory) String() string {
	return f.URL
}

func (f *inputFactory) NewInput(
	ctx context.Context,
) (*kernel.Input, error) {
	return kernel.NewInputFromURL(ctx, f.URL, secret.New(""), kernel.InputConfig{
		KeepOpen:      true,
		ForceRealTime: urltools.IsFileURL(f.URL),
	})
}

func (f *inputFactory) NewDecoderFactory(
	ctx context.Context,
) (codec.DecoderFactory, error) {
	return nil, nil // no decoding
}
