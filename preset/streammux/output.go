package streammux

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	extrapacketcondition "github.com/xaionaro-go/avpipeline/packet/condition/extra"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

type NodeBarrier[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
type NodeMapStreamIndexes[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.MapStreamIndices]]
type NodeRecoder[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]
type OutputKey = types.OutputKey

type Output[C any] struct {
	ID                 int
	InputFilter        *NodeBarrier[C]
	InputThrottler     *packetcondition.VideoAverageBitrateLower
	InputFixer         *autofix.AutoFixer
	RecoderNode        *NodeRecoder[C]
	OutputThrottler    *packetcondition.VideoAverageBitrateLower
	MapIndices         *NodeMapStreamIndexes[C]
	OutputFixer        *autofix.AutoFixer
	OutputSyncer       *NodeBarrier[C]
	OutputNode         node.Abstract
	OutputNodeConfig   OutputConfig
	AutoBitRateHandler *AutoBitRateHandler[C]
}

type OutputConfig struct {
	OutputThrottlerMaxQueueSizeBytes uint64
	AutoBitrate                      *AutoBitRateConfig
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
//	  +------<-------+------->-------+
//	  |                              |
//	  v                              v
//	InputFilter(OutputSwitch)    InputFilter(OutputSwitch)
//	  |                              |
//	  v InputThrottler               v InputThrottler
//	  |                              |
//	  v                              v
//	InputFixer                   InputFixer
//	  |                              |
//	  v                              v
//	dyn(RecoderNode)             dyn(RecoderNode)
//	  |                              |
//	  v OutputThrottler              v OutputThrottler
//	  |                              |
//	  v                              v
//	OutputMapIndices             OutputMapIndices
//	  |                              |
//	  v                              v
//	OutputFixer                  OutputFixer
//	  |                              |
//	  v OutputSyncer-· · · · · · · · v ·-OutputSyncer
//	  |                              |
//	  v                              v
//	OutputNode                   OutputNode

func newOutput[C any](
	ctx context.Context,
	outputID int,
	inputNode *NodeInput[C],
	outputFactory OutputFactory,
	outputKey OutputKey,
	outputSwitch barrierstategetter.StateGetter,
	outputSyncer barrierstategetter.StateGetter,
	streamIndexAssigner kernel.StreamIndexAssigner,
) (_ret *Output[C], _err error) {
	ctx = belt.WithField(ctx, "output_id", outputID)
	ctx = xcontext.DetachDone(ctx)
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Debugf(ctx, "newOutput: %#+v", outputKey)
	defer func() { logger.Debugf(ctx, "/newOutput: %#+v %v", outputKey, _err) }()

	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	// construct nodes

	outputNode, outputConfig, err := outputFactory.NewOutput(ctx, outputKey)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output node: %w", err)
	}

	recoderKernel, err := kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:      codec.Name(outputKey.VideoCodec),
			AudioCodec:      codec.Name(outputKey.AudioCodec),
			VideoResolution: &outputKey.Resolution,
		}),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create recoder kernel: %w", err)
	}

	packetSinker, ok := outputNode.GetProcessor().(processor.GetPacketSinker)
	if !ok {
		return nil, fmt.Errorf("output node %T does not implement GetPacketSinker", outputNode)
	}

	o := &Output[C]{
		ID: outputID,
		InputFilter: node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "InputFilter"),
			outputSwitch,
		)),
		InputThrottler: packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		InputFixer: autofix.New(
			belt.WithField(ctx, "output_chain_step", "InputFixer"),
			inputNode.Processor.GetPacketSource(),
			recoderKernel.Decoder,
		),
		RecoderNode: node.NewWithCustomDataFromKernel[C](
			ctx,
			recoderKernel,
			processor.DefaultOptionsRecoder()...,
		),
		OutputThrottler: packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		MapIndices:      node.NewWithCustomDataFromKernel[C](ctx, kernel.NewMapStreamIndices(ctx, streamIndexAssigner)),
		OutputFixer: autofix.New(
			belt.WithField(ctx, "output_chain_step", "OutputFixer"),
			recoderKernel.Encoder,
			packetSinker.GetPacketSink(),
		),
		OutputSyncer: node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "OutputSyncer"),
			outputSyncer,
		)),
		OutputNode:       outputNode,
		OutputNodeConfig: outputConfig,
	}

	// auto bitrate handler

	if outputConfig.AutoBitrate != nil {
		if len(outputConfig.AutoBitrate.ResolutionsAndBitRates) == 0 {
			return nil, fmt.Errorf("at least one resolution must be specified for automatic bitrate control")
		}
		if queueSizer, ok := outputNode.GetProcessor().(processor.GetInternalQueueSizer); ok {
			h := o.autoBitRateHandler(*outputConfig.AutoBitrate, queueSizer)
			observability.Go(ctx, func(ctx context.Context) {
				defer logger.Debugf(ctx, "autoBitRateHandler(%d).ServeContext(): done", o.ID)
				err := h.ServeContext(ctx)
				logger.Debugf(ctx, "autoBitRateHandler(%d).ServeContext(): %v", o.ID, err)
			})
		}
	}

	// wiring

	o.InputFilter.AddPushPacketsTo(
		o.InputFixer,
		packetfiltercondition.Packet{Condition: o.InputThrottler},
	)
	o.InputFixer.AddPushPacketsTo(o.RecoderNode)
	o.RecoderNode.AddPushPacketsTo(
		o.MapIndices,
	)
	o.MapIndices.AddPushPacketsTo(o.OutputFixer)
	o.OutputFixer.AddPushPacketsTo(o.OutputSyncer)
	maxQueueSizeGetter := mathcondition.GetterFunction[uint64](func() uint64 {
		return o.OutputNodeConfig.OutputThrottlerMaxQueueSizeBytes
	})
	o.OutputSyncer.AddPushPacketsTo(o.OutputNode,
		packetfiltercondition.Packet{Condition: packetcondition.And{
			o.OutputThrottler,
			packetcondition.Or{
				packetcondition.Function(func(context.Context, packet.Input) bool {
					return o.OutputNodeConfig.OutputThrottlerMaxQueueSizeBytes <= 0
				}),
				extrapacketcondition.PushQueueSize(
					o.RecoderNode,
					o.OutputNode,
					mathcondition.LessOrEqualVariable(maxQueueSizeGetter),
				),
			},
		}},
	)

	return o, nil
}

func (o *Output[C]) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output.Close()")
	defer func() { logger.Tracef(ctx, "/Output.Close(): %v", _err) }()
	var errs []error

	if err := o.Flush(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to flush %d: %w", o.ID, err))
	}
	if err := o.InputFilter.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input filter for output %d: %w", o.ID, err))
	}
	if err := o.InputFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input fixer for output %d: %w", o.ID, err))
	}
	if err := o.RecoderNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close recoder node for output %d: %w", o.ID, err))
	}
	if err := o.MapIndices.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close map indices for output %d: %w", o.ID, err))
	}
	if err := o.OutputFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output fixer for output %d: %w", o.ID, err))
	}
	if err := o.OutputSyncer.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output sync filter for output %d: %w", o.ID, err))
	}
	if err := o.OutputNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output node for output %d: %w", o.ID, err))
	}
	if o.AutoBitRateHandler != nil {
		if err := o.AutoBitRateHandler.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close auto bitrate handler for output %d: %w", o.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (o *Output[C]) Input() node.Abstract {
	return o.InputFilter
}

func (o *Output[C]) Output() node.Abstract {
	return o.OutputNode
}

func (o *Output[C]) Flush(ctx context.Context) (_err error) {
	return o.RecoderNode.Processor.Kernel.Reset(ctx)
}

func OutputKeyFromRecoderConfig(
	ctx context.Context,
	c *types.RecoderConfig,
) OutputKey {
	if c == nil {
		return OutputKey{}
	}

	var audioCodec codec.Name
	if len(c.AudioTrackConfigs) > 0 {
		audioCodec = codec.Name(c.AudioTrackConfigs[0].CodecName).Canonicalize(ctx, true)
	}
	var videoCodec codec.Name
	var resolution codec.Resolution
	if len(c.VideoTrackConfigs) > 0 {
		videoCodec = codec.Name(c.VideoTrackConfigs[0].CodecName).Canonicalize(ctx, true)
		resolution = c.VideoTrackConfigs[0].Resolution
	}
	return OutputKey{
		AudioCodec: codectypes.Name(audioCodec),
		VideoCodec: codectypes.Name(videoCodec),
		Resolution: resolution,
	}
}
