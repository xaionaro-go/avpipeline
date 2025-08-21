package autoheaders

import (
	"context"

	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

type NodeWithCustomData[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[kerneltypes.Abstract],
]

type Node = NodeWithCustomData[struct{}]

func NewNode(
	ctx context.Context,
	sink packet.Sink,
) *Node {
	return NewNodeWithCustomData[struct{}](
		ctx,
		sink,
	)
}

func NewNodeWithCustomData[C any](
	ctx context.Context,
	sink packet.Sink,
) *NodeWithCustomData[C] {
	k := NewKernel(ctx, sink)
	n := node.NewWithCustomDataFromKernel[C](ctx, kerneltypes.Abstract(k))
	k.Handler.SetProcessor(n.Processor)
	return n
}
