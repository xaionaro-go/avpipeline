package streammux

import (
	"context"

	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

type InputHandler struct{}

var _ kernelboilerplate.CustomHandlerWithContextFormat = (*InputHandler)(nil)

func (h *InputHandler) String() string {
	return "StreamMuxInput"
}

type NodeInput[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[*kernelboilerplate.BaseWithFormatContext[*InputHandler]],
]

func newInputNode[C any](
	ctx context.Context,
) *NodeInput[C] {
	return node.NewWithCustomDataFromKernel[C](ctx,
		kernelboilerplate.NewKernelWithFormatContext(ctx, &InputHandler{}),
	)
}
