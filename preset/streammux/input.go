package streammux

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
	"github.com/xaionaro-go/avpipeline/processor"
)

type InputType int

const (
	UndefinedInputType = InputType(iota)
	InputTypeAll
	InputTypeAudioOnly
	InputTypeVideoOnly
	EndOfInputType
)

func (t InputType) String() string {
	switch t {
	case UndefinedInputType:
		return "undefined"
	case InputTypeAll:
		return "all"
	case InputTypeAudioOnly:
		return "audio-only"
	case InputTypeVideoOnly:
		return "video-only"
	default:
		return fmt.Sprintf("<unknown_%d>", int(t))
	}
}

func (t InputType) IncludesMediaType(mediaType astiav.MediaType) bool {
	switch t {
	case InputTypeAll:
		return true
	case InputTypeAudioOnly:
		return mediaType == astiav.MediaTypeAudio
	case InputTypeVideoOnly:
		return mediaType == astiav.MediaTypeVideo
	default:
		return false
	}
}

type Input[C any] struct {
	Node               *NodeInput[C]
	MonotonicPTSFilter packetorframecondition.Condition
	OutputSwitch       *barrierstategetter.Switch
	OutputSyncer       *barrierstategetter.Switch
}

func newInput[C any](
	ctx context.Context,
	s *StreamMux[C],
	inputType InputType,
) *Input[C] {
	h := &InputHandler[C]{
		StreamMux: s,
		Type:      inputType,
	}
	k := kernelboilerplate.NewKernelWithFormatContext(ctx, h)
	h.Kernel = k
	return &Input[C]{
		Node:               node.NewWithCustomDataFromKernel[C](ctx, k),
		MonotonicPTSFilter: monotonicpts.New(false),
		OutputSwitch:       barrierstategetter.NewSwitch(),
		OutputSyncer:       barrierstategetter.NewSwitch(),
	}
}

func (i *Input[C]) GetType() InputType {
	return i.Node.Processor.Kernel.Handler.Type
}

type InputHandler[C any] struct {
	StreamMux *StreamMux[C]
	Type      InputType
	Kernel    *kernelboilerplate.BaseWithFormatContext[*InputHandler[C]]
}

var _ kernelboilerplate.CustomHandlerWithContextFormat = (*InputHandler[any])(nil)
var _ kernelboilerplate.StreamFilterer = (*InputHandler[any])(nil)

func (h *InputHandler[C]) String() string {
	return fmt.Sprintf("StreamMux:Input:%s", h.Type)
}

type NodeInput[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[*kernelboilerplate.BaseWithFormatContext[*InputHandler[C]]],
]

func (h *InputHandler[C]) VisitInput(
	ctx context.Context,
	input *packetorframe.InputUnion,
) error {
	if h.StreamMux == nil {
		return nil
	}
	switch h.Type {
	case InputTypeAll:
		err := h.StreamMux.onInput(ctx, *input)
		if err != nil {
			return fmt.Errorf("unable to process input in StreamMux: %w", err)
		}
	}
	return nil
}

func (h *InputHandler[C]) StreamFilter(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	mediaType := inputStream.CodecParameters().MediaType()
	logger.Debugf(ctx, "StreamMux:Input:%s:StreamFilter: %d %s", h.Type, inputStream.Index(), mediaType)
	defer func() {
		logger.Debugf(ctx, "/StreamMux:Input:%s:StreamFilter: %d %s: %v", h.Type, inputStream.Index(), mediaType, _err)
	}()

	if !h.Type.IncludesMediaType(mediaType) {
		return kernelboilerplate.ErrSkip{}
	}
	return nil
}
