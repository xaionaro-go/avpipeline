package boilerplate

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type CustomHandler interface {
	fmt.Stringer
}

type VisitInputFramer interface {
	VisitInputFrame(ctx context.Context, input *frame.Input) error
}

type ErrSkip struct{}

func (ErrSkip) Error() string {
	return "skip this frame/packet"
}

type AmendOutputFramer interface {
	AmendOutputFrame(ctx context.Context, output *frame.Output) error
}

type VisitInputPacketer interface {
	VisitInputPacket(ctx context.Context, input *packet.Input) error
}

type AmendOutputPacketer interface {
	AmendOutputPacket(ctx context.Context, output *packet.Output) error
}

type Base[H CustomHandler] struct {
	*closuresignaler.ClosureSignaler
	Handler       H
	Locker        xsync.Mutex
	FormatContext *astiav.FormatContext
	OutputStreams map[int]*astiav.Stream
}

var _ types.Abstract = (*Base[CustomHandler])(nil)

func NewBasicKernel[H CustomHandler](
	ctx context.Context,
	handler H,
) *Base[H] {
	logger.Tracef(ctx, "NewBasicKernel")
	defer func() { logger.Tracef(ctx, "/NewBasicKernel") }()
	k := &Base[H]{
		Handler:         handler,
		ClosureSignaler: closuresignaler.New(),
		FormatContext:   astiav.AllocFormatContext(),
		OutputStreams:   map[int]*astiav.Stream{},
	}
	setFinalizerFree(ctx, k.FormatContext)
	return k
}

func (k *Base[H]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket()")
	defer func() { logger.Tracef(ctx, "/SendInputPacket(): %v", _err) }()

	if sender, ok := any(k.Handler).(types.SendInputPacketer); ok {
		return sender.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	}

	if visitor, ok := any(k.Handler).(VisitInputPacketer); ok {
		if err := visitor.VisitInputPacket(ctx, &input); err != nil {
			return prepareError(ctx, err, "visit input packet")
		}
	}

	outPkt := packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.StreamInfo,
	)

	if sender, ok := any(k.Handler).(AmendOutputPacketer); ok {
		if err := sender.AmendOutputPacket(ctx, &outPkt); err != nil {
			return prepareError(ctx, err, "amend output packet")
		}
	}

	select {
	case outputPacketsCh <- outPkt:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (k *Base[H]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame()")
	defer func() { logger.Tracef(ctx, "/SendInputFrame(): %v", _err) }()

	if sender, ok := any(k.Handler).(types.SendInputFramer); ok {
		return sender.SendInputFrame(ctx, input, outputPacketsCh, outputFramesCh)
	}

	if visitor, ok := any(k.Handler).(VisitInputFramer); ok {
		if err := visitor.VisitInputFrame(ctx, &input); err != nil {
			return prepareError(ctx, err, "visit input frame")
		}
	}

	outFrame := frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.StreamInfo,
	)

	if amender, ok := any(k.Handler).(AmendOutputFramer); ok {
		if err := amender.AmendOutputFrame(ctx, &outFrame); err != nil {
			return prepareError(ctx, err, "amend output frame")
		}
	}

	select {
	case outputFramesCh <- outFrame:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func prepareError(ctx context.Context, err error, action string) error {
	if err == nil {
		return nil
	}
	switch err := err.(type) {
	case ErrSkip:
		logger.Tracef(ctx, "got a skip signal from '%s': %v", action, err)
		return nil
	default:
		return fmt.Errorf("unable to '%s': %w", action, err)
	}
}

func (k *Base[H]) String() string {
	return k.Handler.String()
}

func (k *Base[H]) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close()")
	defer func() { logger.Tracef(ctx, "/Close(): %v", _err) }()
	k.ClosureSignaler.Close(ctx)
	return nil
}

func (k *Base[H]) CloseChan() <-chan struct{} {
	return k.ClosureSignaler.CloseChan()
}

func (k *Base[H]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "Generate()")
	defer func() { logger.Tracef(ctx, "/Generate(): %v", _err) }()

	if sender, ok := any(k.Handler).(types.Generator); ok {
		return sender.Generate(ctx, outputPacketsCh, outputFramesCh)
	}

	return nil
}

