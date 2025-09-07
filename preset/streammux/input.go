package streammux

import (
	"context"

	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

type InputHandler[C any] struct {
	StreamMux *StreamMux[C]
}

var _ kernelboilerplate.CustomHandlerWithContextFormat = (*InputHandler[any])(nil)

func (h *InputHandler[C]) String() string {
	return "StreamMuxInput"
}

type NodeInput[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[*kernelboilerplate.BaseWithFormatContext[*InputHandler[C]]],
]

func newInputNode[C any](
	ctx context.Context,
	s *StreamMux[C],
) *NodeInput[C] {
	return node.NewWithCustomDataFromKernel[C](ctx,
		kernelboilerplate.NewKernelWithFormatContext(ctx, &InputHandler[C]{
			StreamMux: s,
		}),
	)
}

func (h *InputHandler[C]) VisitInputPacket(
	ctx context.Context,
	input *packet.Input,
) error {
	if h.StreamMux == nil {
		return nil
	}
	return h.StreamMux.onInputPacket(ctx, input)
}
