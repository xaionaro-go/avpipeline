package streammux

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

type OutputKey = types.OutputKey

type Output[C any] struct {
	InputSyncFilter  *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	InputThrottler   *packetcondition.VideoAverageBitrateLower
	InputFixer       *autofix.AutoFixer
	RecoderNode      *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]
	OutputThrottler  *packetcondition.VideoAverageBitrateLower
	OutputMapIndices *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.MapStreamIndices]]
	OutputFixer      *autofix.AutoFixer
	OutputSyncFilter *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	OutputNode       node.Abstract
}

type RetryParameters struct {
}

type initOutputConfig struct {
	RetryParameters *RetryParameters
}

type InitOutputOption interface {
	apply(*initOutputConfig)
}

type InitOutputOptions []InitOutputOption

func (opts InitOutputOptions) apply(cfg *initOutputConfig) {
	for _, opt := range opts {
		opt.apply(cfg)
	}
}

func (opts InitOutputOptions) config() initOutputConfig {
	cfg := initOutputConfig{}
	opts.apply(&cfg)
	return cfg
}

type InitOutputOptionRetry *RetryParameters

// An example with two Outputs:
//
//                 Input
//                   |
//                   v
// InputSyncFilter-· | ·-InputSyncFilter
//	  +------<-------+------->-------+
//	  |                              |
//	  v                              v
//	InputThrottler                InputThrottler
//	  |                              |
//	  v                              v
//	InputFixer                   InputFixer
//	  |                              |
//	  v                              v
//	dyn(RecoderNode)             dyn(RecoderNode)
//	  |                              |
//	  v                              v
//	OutputThrottler              OutputThrottler
//	  |                              |
//	  v                              v
//	OutputMapIndices             OutputMapIndices
//	  |                              |
//	  v                              v
//	OutputFixer                  OutputFixer
//	  |                              |
//	  v OutputSyncFilter-· · · · · · v ·-OutputSyncFilter
//	  |                              |
//	  v                              v
//	OutputNode                   OutputNode

func newOutput[C any](
	ctx context.Context,
	inputNode *InputNode[C],
	outputFactory OutputFactory,
	outputKey OutputKey,
	inputSyncer barrierstategetter.StateGetter,
	outputSyncer barrierstategetter.StateGetter,
	streamIndexAssigner kernel.StreamIndexAssigner,
) (_ret *Output[C], _err error) {
	logger.Tracef(ctx, "NewOutput: %#+v", outputKey)
	defer func() { logger.Tracef(ctx, "/NewOutput: %#+v", outputKey, _err) }()

	outputNode, err := outputFactory.NewOutput(ctx, outputKey)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output node: %w", err)
	}

	recoderKernel, err := kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		codec.NewNaiveEncoderFactory(ctx, nil),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create recoder kernel: %w", err)
	}

	outputProcessor, ok := outputNode.GetProcessor().(processor.GetPacketSinker)
	if !ok {
		return nil, fmt.Errorf("output node %T does not implement GetPacketSinker", outputNode)
	}

	recoder := node.NewWithCustomDataFromKernel[C](
		ctx,
		recoderKernel,
		processor.DefaultOptionsRecoder()...,
	)
	o := &Output[C]{
		InputSyncFilter: node.NewWithCustomData[C](processor.NewFromKernel(ctx, kernel.NewBarrier(ctx, inputSyncer))),
		InputThrottler:  packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		InputFixer: autofix.New(
			ctx,
			inputNode.Processor.GetPacketSource(),
			recoderKernel.Decoder,
		),
		RecoderNode:      recoder,
		OutputThrottler:  packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		OutputMapIndices: node.NewWithCustomDataFromKernel[C](ctx, kernel.NewMapStreamIndices(ctx, streamIndexAssigner)),
		OutputFixer: autofix.New(
			ctx,
			recoderKernel.Encoder,
			outputProcessor.GetPacketSink(),
		),
		OutputSyncFilter: node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(ctx, outputSyncer)),
		OutputNode:       outputNode,
	}

	o.InputSyncFilter.AddPushPacketsTo(
		o.InputFixer,
		packetfiltercondition.Packet{Condition: o.InputThrottler},
	)
	o.InputFixer.AddPushPacketsTo(o.RecoderNode)
	o.RecoderNode.AddPushPacketsTo(
		o.OutputMapIndices,
		packetfiltercondition.Packet{Condition: o.OutputThrottler},
	)
	o.OutputMapIndices.AddPushPacketsTo(o.OutputFixer)
	o.OutputFixer.AddPushPacketsTo(o.OutputSyncFilter)
	o.OutputSyncFilter.AddPushPacketsTo(o.OutputNode)

	return o, nil
}

func (o *Output[C]) Input() node.Abstract {
	return o.InputSyncFilter
}

func (o *Output[C]) Output() node.Abstract {
	return o.OutputNode
}

func (o *Output[C]) Flush(ctx context.Context) (_err error) {
	return o.RecoderNode.Processor.Kernel.Reset(ctx)
}
