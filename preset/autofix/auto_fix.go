package autofix

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	"github.com/xaionaro-go/avpipeline/processor"
)

type AutoFixer[T any] struct {
	AutoHeadersNode      *autoheaders.NodeWithCustomData[T]
	MapStreamIndicesNode *node.NodeWithCustomData[T, *processor.FromKernel[*kernel.MapStreamIndices]]
}

var _ node.DotBlockContentStringWriteToer = (*AutoFixer[any])(nil)

func New(
	ctx context.Context,
	forInput packet.Source,
	forOutput packet.Sink,
) *AutoFixer[struct{}] {
	return NewWithCustomData[struct{}](
		ctx,
		forInput,
		forOutput,
		struct{}{},
	)
}

func NewWithCustomData[T any](
	ctx context.Context,
	forInput packet.Source,
	forOutput packet.Sink,
	customData T,
) *AutoFixer[T] {
	var zeroT T
	logger.Debugf(ctx, "New[%T]: %s %s", zeroT, forInput, forOutput)

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
	var streamIndexAssigner kernel.StreamIndexAssigner
	switch outputFormatName {
	case "flv":
		streamIndexAssigner = streamIndexAssignerFLV{}
	}

	a := &AutoFixer[T]{
		AutoHeadersNode: autoheaders.NewNodeWithCustomData[T](ctx, forInput, forOutput),
		MapStreamIndicesNode: node.NewWithCustomData[T](
			processor.NewFromKernel(
				ctx,
				kernel.NewMapStreamIndices(ctx, streamIndexAssigner),
				processor.DefaultOptionsRecoder()...,
			),
		),
	}
	a.MapStreamIndicesNode.CustomData = customData
	if a.AutoHeadersNode != nil {
		a.AutoHeadersNode.AddPushPacketsTo(a.MapStreamIndicesNode)
		a.AutoHeadersNode.CustomData = customData
	}
	return a
}

func (a *AutoFixer[T]) Input() node.Abstract {
	if a.AutoHeadersNode != nil {
		return a.AutoHeadersNode
	}
	return a.MapStreamIndicesNode
}

func (a *AutoFixer[T]) Output() *node.NodeWithCustomData[T, *processor.FromKernel[*kernel.MapStreamIndices]] {
	return a.MapStreamIndicesNode
}
