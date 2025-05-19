package autoheaders

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

type NodeWithCustomData[T any] = node.NodeWithCustomData[T, *processor.FromKernel[*kernel.BitstreamFilter]]
type Node = NodeWithCustomData[struct{}]

func NewNode(
	ctx context.Context,
	forInput packet.Source,
	forOutput packet.Sink,
) *Node {
	return NewNodeWithCustomData[struct{}](
		ctx,
		forInput,
		forOutput,
	)
}

func NewNodeWithCustomData[T any](
	ctx context.Context,
	forInput packet.Source,
	forOutput packet.Sink,
) *NodeWithCustomData[T] {
	var zeroT T
	logger.Debugf(ctx, "NewNode[%T]: %s %s", zeroT, forInput, forOutput)
	var inputFormatName string
	inputAmountOfStreams := 0
	inputHasExtraData := false
	forInput.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx == nil {
			logger.Errorf(ctx, "the output has no format context")
			return
		}
		inputAmountOfStreams = fmtCtx.NbStreams()
		for _, stream := range fmtCtx.Streams() {
			if len(stream.CodecParameters().ExtraData()) > 0 {
				inputHasExtraData = true
			}
		}
		fmt := fmtCtx.OutputFormat()
		if fmt == nil {
			logger.Debugf(ctx, "the output has no format (an intermediate node, not an actual output?)")
			return
		}
		inputFormatName = fmt.Name()
	})
	logger.Debugf(ctx, "input format: '%s' (has extra_data: %t; amount of streams: %d; source: %T)", inputFormatName, inputHasExtraData, inputAmountOfStreams, forInput)

	var outputFormatName string
	forOutput.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx == nil {
			logger.Errorf(ctx, "the output has no format context")
			return
		}
		fmt := fmtCtx.OutputFormat()
		if fmt == nil {
			logger.Debugf(ctx, "the output has no format (an intermediate node, not an actual output?)")
			return
		}
		outputFormatName = fmt.Name()
	})
	logger.Debugf(ctx, "output format: '%s'", outputFormatName)

	isOOBHeadersInput := isOOBHeadersByFormatName(ctx, inputFormatName) || inputHasExtraData
	isOOBHeadersOutput := isOOBHeadersByFormatName(ctx, outputFormatName)
	logger.Debugf(ctx, "isOOBHeaders: input:%t output:%t", isOOBHeadersInput, isOOBHeadersOutput)

	switch {
	case isOOBHeadersInput == isOOBHeadersOutput:
		return nil
	case isOOBHeadersOutput:
		return tryNewBSFForOOBHeaders[T](ctx)
	case isOOBHeadersInput:
		return tryNewBSFForInBandHeaders[T](ctx)
	default:
		panic("this is supposed to be impossible")
	}
}
