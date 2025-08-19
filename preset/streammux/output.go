package streammux

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

type OutputKey = types.OutputKey

type Output struct {
	InputSyncFilter  *node.NodeWithCustomData[*processor.FromKernel[*kernel.Barrier]]
	InputThrottler   *packetcondition.VideoAverageBitrateLower
	InputFixer       *autofix.AutoFixer
	RecoderNode      *kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	OutputThrottler  *packetcondition.VideoAverageBitrateLower
	OutputMapIndices *kernel.MapStreamIndices
	OutputFixer      *autofix.AutoFixer
	OutputSyncFilter *node.NodeWithCustomData[*processor.FromKernel[*kernel.Barrier]]
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

/*
//
//              Input
//                |
//                v
InputSyncFilter-· | ·-InputSyncFilter

	  +------<-------+------->-------+
	  |                              |
	  v                              v
	InputThrottler                InputThrottler
	  |                              |
	  v                              v
	InputFixer                   InputFixer
	  |                              |
	  v                              v
	dyn(RecoderNode)             dyn(RecoderNode)
	  |                              |
	  v                              v
	OutputThrottler              OutputThrottler
	  |                              |
	  v                              v
	OutputMapIndices             OutputMapIndices
	  |                              |
	  v                              v
	OutputFixer                  OutputFixer
	  |                              |
	  v OutputSyncFilter-· · · · · · v ·-OutputSyncFilter
	  |                              |
	  v                              v
	OutputNode                   OutputNode
*/
func NewOutput(
	ctx context.Context,
	n node.Abstract,
	inputSyncFilter *packetcondition.SwitchPacketCondition,
	outputSyncFilter *packetcondition.SwitchPacketCondition,
) (_ret *Output, _err error) {
	logger.Tracef(ctx, "NewOutput()")
	defer func() { logger.Tracef(ctx, "/NewOutput(): %v", _err) }()
	o := &Output{
		InputSyncFilter:  node.New(processor.NewFromKernel(ctx, kernel.NewBarrier(inputSyncFilter))),
		OutputSyncFilter: node.New(processor.NewFromKernel(ctx, kernel.NewBarrier(outputSyncFilter))),
		OutputNode:       n,
	}
	return o, nil
}
