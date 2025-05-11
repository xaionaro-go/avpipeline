//go:build none
// +build none

package transcoder

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition/extra"
	"github.com/xaionaro-go/avpipeline/processor"
)

type outputStreamSourceChangeHandler[C any, P processor.Abstract] struct {
	*StreamForward[C, P]
}

var _ extra.OnStreamSourceChangeHandler = (*outputStreamSourceChangeHandler[any, processor.Abstract])(nil)

func newOutputStreamSourceChangeHandler[C any, P processor.Abstract](
	s *StreamForward[C, P],
) *outputStreamSourceChangeHandler[C, P] {
	return &outputStreamSourceChangeHandler[C, P]{
		FFStream: s,
	}
}

func (h *outputStreamSourceChangeHandler[C, P]) OnStreamSourceChange(
	ctx context.Context,
	pkt packet.Input,
	prevSource packet.Source,
) {
	h.nodeOutput.Flush()
	h.nodeOutput.Processor.Kernel.DeleteStream(ctx, pkt.StreamIndex())
}
