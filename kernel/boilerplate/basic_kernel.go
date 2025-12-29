package boilerplate

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type CustomHandler interface {
	fmt.Stringer
}

type VisitInputer interface {
	VisitInput(ctx context.Context, input *packetorframe.InputUnion) error
}

type ErrSkip struct{}

func (ErrSkip) Error() string {
	return "skip this frame/packet"
}

type AmendOutputer interface {
	AmendOutput(ctx context.Context, output *packetorframe.OutputUnion) error
}

type Base[H CustomHandler] struct {
	*closuresignaler.ClosureSignaler
	Handler       H
	Locker        xsync.Mutex
	FormatContext *astiav.FormatContext
	OutputStreams map[int]*astiav.Stream
}

var _ kerneltypes.Abstract = (*Base[CustomHandler])(nil)

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

func (k *Base[H]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput()")
	defer func() { logger.Tracef(ctx, "/SendInput(): %v", _err) }()

	if sender, ok := any(k.Handler).(kerneltypes.SendInputer); ok {
		return sender.SendInput(ctx, input, outputCh)
	}

	if visitor, ok := any(k.Handler).(VisitInputer); ok {
		if err := visitor.VisitInput(ctx, &input); err != nil {
			return prepareError(ctx, err, "visit input")
		}
	}

	out := input.CloneAsReferencedOutput()

	if amender, ok := any(k.Handler).(AmendOutputer); ok {
		if err := amender.AmendOutput(ctx, &out); err != nil {
			return prepareError(ctx, err, "amend output")
		}
	}

	if out.Packet != nil || out.Frame != nil {
		select {
		case outputCh <- out:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func ptr[T any](v T) *T {
	return &v
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

func (k *Base[H]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Base[H]) String() string {
	if k == nil {
		return "<BasicKernel:nil>"
	}
	return k.Handler.String()
}

func (k *Base[H]) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close()")
	defer func() { logger.Tracef(ctx, "/Close(): %v", _err) }()
	k.ClosureSignaler.Close(ctx)
	if close, ok := any(k.Handler).(types.Closer); ok {
		return close.Close(ctx)
	}
	return nil
}

func (k *Base[H]) CloseChan() <-chan struct{} {
	return k.ClosureSignaler.CloseChan()
}

func (k *Base[H]) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "Generate()")
	defer func() { logger.Tracef(ctx, "/Generate(): %v", _err) }()

	if sender, ok := any(k.Handler).(kerneltypes.Generator); ok {
		return sender.Generate(ctx, outputCh)
	}

	return nil
}
