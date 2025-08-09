package screencapturer

import (
	"context"
	"fmt"
	"image"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

type ScreenCapturer[C any] struct {
	InputNode   *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Input]]
	DecoderNode *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Decoder[*codec.NaiveDecoderFactory]]]
}

var _ node.DotBlockContentStringWriteToer = (*ScreenCapturer[any])(nil)
var _ node.Abstract = (*ScreenCapturer[any])(nil)

type Params struct {
	Area image.Rectangle
	FPS  astiav.Rational
}

func New(
	ctx context.Context,
	params Params,
) (*ScreenCapturer[struct{}], error) {
	return NewWithCustomData[struct{}](ctx, params)
}

func NewWithCustomData[C any](
	ctx context.Context,
	params Params,
) (*ScreenCapturer[C], error) {
	var opts types.DictionaryItems

	opts = append(opts, types.DictionaryItem{
		Key:   "framerate",
		Value: fmt.Sprintf("%f", params.FPS.Float64()),
	})
	opts = append(opts, inputOptions(params)...)

	inputKernel, err := kernel.NewInputFromURL(
		ctx,
		getScreenGrabInput(params),
		secret.String{},
		kernel.InputConfig{
			CustomOptions: opts,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the screen-grab input: %w", err)
	}
	inputNode := node.NewWithCustomDataFromKernel[C](ctx, inputKernel)

	decoderKernel := kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx, nil))
	decoderNode := node.NewWithCustomDataFromKernel[C](ctx, decoderKernel)

	inputNode.AddPushPacketsTo(decoderNode)

	return &ScreenCapturer[C]{
		InputNode:   inputNode,
		DecoderNode: decoderNode,
	}, nil
}

func (a *ScreenCapturer[C]) Input() *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Input]] {
	return a.InputNode
}

func (a *ScreenCapturer[C]) Output() *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Decoder[*codec.NaiveDecoderFactory]]] {
	return a.DecoderNode
}
