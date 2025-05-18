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

type Node[T any] = node.NodeWithCustomData[T, *processor.FromKernel[*kernel.BitstreamFilter]]

func NewNode(
	ctx context.Context,
	forInput packet.Source,
	forOutput packet.Sink,
) *Node[struct{}] {
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
) *Node[T] {
	var outputFormatName string
	forOutput.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx == nil {
			logger.Errorf(ctx, "the output has no format context")
			return
		}
		outputFmt := fmtCtx.OutputFormat()
		if outputFmt == nil {
			logger.Debugf(ctx, "the output has no format (an intermediate node, not an actual output?)")
			return
		}
		outputFormatName = outputFmt.Name()
	})
	logger.Infof(ctx, "output format: '%s'", outputFormatName)

	var inputVideoCodecID, inputAudioCodecID astiav.CodecID
	forInput.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		inputVideoCodecID, inputAudioCodecID = getCodecNamesFromStreams(
			fmtCtx.Streams(),
		)
	})

	switch outputFormatName {
	default:
		logger.Debugf(ctx, "the output format is unknown, so defaulting to in-band headers")
		fallthrough
	case "mpegts", "rtsp":
		return tryNewBSFForInBandHeaders[T](ctx, inputVideoCodecID, inputAudioCodecID)
	case "flv":
		return tryNewBSFForOOBHeaders[T](ctx, inputVideoCodecID, inputAudioCodecID)
	}
}
