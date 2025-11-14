package boilerplate

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type CustomHandlerWithContextFormat interface {
	CustomHandler
}

type BaseWithFormatContext[H CustomHandlerWithContextFormat] struct {
	*Base[H]
	FormatContext *astiav.FormatContext
	OutputStreams map[int]*astiav.Stream
}

var _ types.Abstract = (*BaseWithFormatContext[CustomHandlerWithContextFormat])(nil)
var _ packet.Source = (*BaseWithFormatContext[CustomHandlerWithContextFormat])(nil)
var _ packet.Sink = (*BaseWithFormatContext[CustomHandlerWithContextFormat])(nil)

func NewKernelWithFormatContext[H CustomHandlerWithContextFormat](
	ctx context.Context,
	handler H,
) *BaseWithFormatContext[H] {
	logger.Tracef(ctx, "NewKernelWithFormatContext")
	defer func() { logger.Tracef(ctx, "/NewKernelWithFormatContext") }()
	k := &BaseWithFormatContext[H]{
		Base:          NewBasicKernel(ctx, handler),
		FormatContext: astiav.AllocFormatContext(),
		OutputStreams: map[int]*astiav.Stream{},
	}
	setFinalizerFree(ctx, k.FormatContext)
	return k
}

func (k *BaseWithFormatContext[H]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *BaseWithFormatContext[H]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Debugf(ctx, "WithOutputFormatContext")
	defer func() { logger.Debugf(ctx, "/WithOutputFormatContext") }()

	if handler, ok := any(k.Handler).(packet.Source); ok {
		handler.WithOutputFormatContext(ctx, callback)
		return
	}

	k.Locker.Do(ctx, func() {
		callback(k.FormatContext)
	})
}

func (k *BaseWithFormatContext[H]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Debugf(ctx, "WithInputFormatContext")
	defer func() { logger.Debugf(ctx, "/WithInputFormatContext") }()

	if handler, ok := any(k.Handler).(packet.WithInputFormatContexter); ok {
		handler.WithInputFormatContext(ctx, callback)
		return
	}

	k.Locker.Do(ctx, func() {
		callback(k.FormatContext)
	})
}

type StreamFilterer interface {
	StreamFilter(ctx context.Context, inputStream *astiav.Stream) error
}

func (k *BaseWithFormatContext[H]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_ret error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _ret) }()

	if handler, ok := any(k.Handler).(packet.NotifyAboutPacketSourcer); ok {
		handler.NotifyAboutPacketSource(ctx, source)
		return
	}

	var errs []error
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		k.Locker.Do(ctx, func() {
			for _, inputStream := range fmtCtx.Streams() {
				if handler, ok := any(k.Handler).(StreamFilterer); ok {
					err := handler.StreamFilter(ctx, inputStream)
					switch {
					case errors.Is(err, ErrSkip{}):
						logger.Debugf(ctx, "skipping stream #%d from source %T as per ErrSkip", inputStream.Index(), source)
						continue
					case err != nil:
						errs = append(errs, fmt.Errorf("unable to filter stream #%d from source %T: %w", inputStream.Index(), source, err))
						continue
					}
				}

				outputStream, err := k.getOutputStreamForPacketByIndex(
					ctx,
					inputStream.Index(),
					inputStream.CodecParameters(),
					inputStream.TimeBase(),
				)
				if outputStream != nil {
					logger.Debugf(ctx, "made sure stream #%d (<-%d) is initialized", outputStream.Index(), inputStream.Index())
				} else {
					logger.Debugf(ctx, "no output stream for stream <-%d", inputStream.Index())
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream #%d for input stream %d from source %s: %w", inputStream.Index(), inputStream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (k *BaseWithFormatContext[H]) getOutputStreamForPacketByIndex(
	ctx context.Context,
	outputStreamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := k.OutputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	outputStream, err := k.newOutputStream(
		ctx,
		outputStreamIndex,
		codecParameters, timeBase,
	)
	if err != nil {
		return nil, err
	}
	k.OutputStreams[outputStreamIndex] = outputStream
	return outputStream, nil
}

func (k *BaseWithFormatContext[H]) newOutputStream(
	ctx context.Context,
	outputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := k.FormatContext.NewStream(nil)
	codecParams.Copy(outputStream.CodecParameters())
	outputStream.SetTimeBase(timeBase)
	outputStream.SetIndex(outputStreamIndex)
	logger.Debugf(
		ctx,
		"new output stream %d: %s: %s: %s: %s: %s",
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	return outputStream, nil
}

func (k *BaseWithFormatContext[H]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket()")
	defer func() { logger.Tracef(ctx, "/SendInputPacket(): %v", _err) }()

	outputStream, err := xsync.DoR2(ctx, &k.Locker, func() (*astiav.Stream, error) {
		return k.getOutputStreamForPacketByIndex(
			ctx,
			input.Stream.Index(),
			input.Stream.CodecParameters(),
			input.Stream.TimeBase(),
		)
	})
	if err != nil {
		return fmt.Errorf("unable to get output stream for input packet: %w", err)
	}
	assert(ctx, outputStream != nil, "output stream must be initialized")

	input.Source = k
	input.Stream = outputStream
	return k.Base.SendInputPacket(
		ctx,
		input,
		outputPacketsCh,
		outputFramesCh,
	)
}
