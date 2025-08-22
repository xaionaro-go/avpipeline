package autofix

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	"github.com/xaionaro-go/avpipeline/processor"
)

type AutoFixer = AutoFixerWithCustomData[DefaultCustomData]

type AutoFixerWithCustomData[T any] struct {
	AutoHeadersNode      *autoheaders.NodeWithCustomData[T]
	MapStreamIndicesNode *node.NodeWithCustomData[T, *processor.FromKernel[*kernel.MapStreamIndices]]
}

var _ node.DotBlockContentStringWriteToer = (*AutoFixerWithCustomData[any])(nil)

type DefaultCustomData struct{}

func New(
	ctx context.Context,
	source packet.Source,
	sink packet.Sink,
) *AutoFixer {
	return NewWithCustomData[DefaultCustomData](
		ctx,
		source,
		sink,
		struct{}{},
	)
}

func NewWithCustomData[T any](
	ctx context.Context,
	source packet.Source,
	sink packet.Sink,
	customData T,
) *AutoFixerWithCustomData[T] {
	var zeroT T
	logger.Debugf(ctx, "New[%T]: %s %s", zeroT, source, sink)

	var outputFormatName string
	sink.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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

	a := &AutoFixerWithCustomData[T]{
		AutoHeadersNode: autoheaders.NewNodeWithCustomData[T](ctx, sink),
		MapStreamIndicesNode: node.NewWithCustomData[T](
			processor.NewFromKernel(
				ctx,
				kernel.NewMapStreamIndices(ctx, streamIndexAssigner),
				processor.DefaultOptionsRecoder()...,
			),
		),
	}
	a.MapStreamIndicesNode.CustomData = customData
	if a.AutoHeadersNode != nil { // TODO: make a.AutoHeadersNode always non-nil
		a.AutoHeadersNode.AddPushPacketsTo(a.MapStreamIndicesNode)
		a.AutoHeadersNode.AddPushFramesTo(a.MapStreamIndicesNode)
		a.AutoHeadersNode.CustomData = customData
	}
	return a
}

func (a *AutoFixerWithCustomData[T]) Input() node.Abstract {
	if a.AutoHeadersNode != nil {
		return a.AutoHeadersNode
	}
	return a.MapStreamIndicesNode
}

func (a *AutoFixerWithCustomData[T]) Output() *node.NodeWithCustomData[T, *processor.FromKernel[*kernel.MapStreamIndices]] {
	return a.MapStreamIndicesNode
}
