package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/stream"
	"github.com/xaionaro-go/xsync"
)

type Encoder[EF codec.EncoderFactory] struct {
	EncoderFactory EF

	*closeChan
	encoders         map[int]*streamEncoder
	streamConfigurer StreamConfigurer

	outputFormatContextLocker xsync.RWMutex
	outputFormatContext       *astiav.FormatContext
	outputStreams             map[int]*astiav.Stream
}

var _ Abstract = (*Encoder[codec.EncoderFactory])(nil)

type streamEncoder struct {
	codec.Encoder
	LastInitTS time.Time
}

func NewEncoder[EF codec.EncoderFactory](
	ctx context.Context,
	encoderFactory EF,
	streamConfigurer StreamConfigurer,
) *Encoder[EF] {
	e := &Encoder[EF]{
		closeChan:           newCloseChan(),
		EncoderFactory:      encoderFactory,
		encoders:            map[int]*streamEncoder{},
		streamConfigurer:    streamConfigurer,
		outputFormatContext: astiav.AllocFormatContext(),
		outputStreams:       make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, e.outputFormatContext)
	return e
}

func (e *Encoder[EF]) Close(ctx context.Context) error {
	e.closeChan.Close(ctx)
	for key, encoder := range e.encoders {
		err := encoder.Close(ctx)
		logger.Debugf(ctx, "encoder closed: %v", err)
		delete(e.encoders, key)
	}
	return nil
}

func (e *Encoder[EF]) initOutputStreamCopy(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStreamCopy: streamIndex: %d", inputStream.Index())
	defer func() {
		if _err == nil {
			assert(ctx, e.outputStreams[inputStream.Index()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStreamCopy: streamIndex: %d: %v", inputStream.Index(), _err)
	}()
	logger.Debugf(ctx, "new output (copy) stream (stream index: %d)", inputStream.Index())

	codecParams := inputStream.CodecParameters()
	codec := astiav.FindDecoder(codecParams.CodecID())
	var outputStream *astiav.Stream
	e.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = e.outputFormatContext.NewStream(codec)
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an (copy) output stream")
	}
	err := codecParams.Copy(outputStream.CodecParameters())
	if err != nil {
		return fmt.Errorf("unable to copy codec parameters: %w", err)
	}

	err = e.configureOutputStream(ctx, outputStream, inputStream)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) initOutputStream(
	ctx context.Context,
	inputStream *astiav.Stream,
	encoder codec.Encoder,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStream: streamIndex: %d", inputStream.Index())
	defer func() {
		if _err == nil {
			assert(ctx, e.outputStreams[inputStream.Index()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStream: streamIndex: %d: %v", inputStream.Index(), _err)
	}()

	logger.Debugf(ctx, "new output stream (stream index: %d)", inputStream.Index())

	var outputStream *astiav.Stream
	e.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = e.outputFormatContext.NewStream(encoder.Codec())
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an output stream")
	}
	if err := encoder.ToCodecParameters(outputStream.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy codec parameters from the encoder to the output stream: %w", err)
	}

	err := e.configureOutputStream(ctx, outputStream, inputStream)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) configureOutputStream(
	ctx context.Context,
	outputStream *astiav.Stream,
	inputStream *astiav.Stream,
) error {
	outputStream.SetIndex(inputStream.Index())
	stream.CopyNonCodecParameters(outputStream, inputStream)
	if e.streamConfigurer != nil {
		err := e.streamConfigurer.StreamConfigure(ctx, outputStream, inputStream)
		if err != nil {
			return fmt.Errorf("unable to configure the output stream: %w", err)
		}
	}
	logger.Tracef(
		ctx,
		"resulting output stream for input stream %d: %d: %s: %s: %s: %s: %s",
		inputStream.Index(),
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	e.outputStreams[inputStream.Index()] = outputStream
	assert(ctx, outputStream != nil)

	return nil
}

func (e *Encoder[EF]) updateOutputs(
	ctx context.Context,
	inputFmt *astiav.FormatContext,
) (_err error) {
	logger.Debugf(ctx, "updateEncoders()")
	defer func() { logger.Debugf(ctx, "/updateEncoders(): %v", _err) }()
	for _, inputStream := range inputFmt.Streams() {
		inputStreamIndex := inputStream.Index()
		if _, ok := e.encoders[inputStreamIndex]; ok {
			logger.Tracef(ctx, "stream #%d already exists, not initializing", inputStreamIndex)
			continue
		}

		err := e.initEncoderFor(ctx, inputStream)
		if err != nil {
			return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", inputStreamIndex, err)
		}

		encoder := e.encoders[inputStreamIndex]
		if encoder.Encoder == (codec.EncoderCopy{}) {
			err = e.initOutputStreamCopy(ctx, inputStream)
		} else {
			err = e.initOutputStream(ctx, inputStream, encoder)
		}
		if err != nil {
			return fmt.Errorf("unable to init an output stream for encoder %s for input stream #%d: %w", encoder, inputStreamIndex, err)
		}
	}

	return nil
}

func (e *Encoder[EF]) initEncoderFor(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	logger.Debugf(ctx, "initEncoderFor(ctx, stream[%d])", inputStream.Index())
	defer func() { logger.Debugf(ctx, "/initEncoderFor(ctx, stream[%d]): %v", inputStream.Index(), _err) }()

	encoderInstance, err := e.EncoderFactory.NewEncoder(ctx, inputStream)
	if err != nil {
		return fmt.Errorf("cannot initialize an encoder for stream %d: %w", inputStream.Index(), err)
	}

	encoder := &streamEncoder{Encoder: encoderInstance}
	e.encoders[inputStream.Index()] = encoder
	return nil
}

func (e *Encoder[EF]) String() string {
	return fmt.Sprintf("Encoder(%s)", e.EncoderFactory)
}

func (e *Encoder[EF]) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

type ErrNotCopyEncoder struct{}

func (ErrNotCopyEncoder) Error() string {
	return "one cannot send undecoded packets via an encoder; it is required to decode them first and send as raw frames"
}

func (r *Encoder[EF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	encoder := r.encoders[input.GetStreamIndex()]
	logger.Tracef(ctx, "encoder == %v", encoder)
	if encoder == nil {
		err := r.updateOutputs(ctx, input.FormatContext)
		if err != nil {
			return fmt.Errorf("unable to update outputs: %w", err)
		}
		encoder = r.encoders[input.GetStreamIndex()]
	}
	assert(ctx, encoder != nil)

	if encoder.Encoder != (codec.EncoderCopy{}) {
		return ErrNotCopyEncoder{}
	}

	outputStream := r.outputStreams[input.GetStreamIndex()]
	assert(ctx, outputStream != nil)
	pkt := packet.CloneAsReferenced(input.Packet)
	pkt.SetStreamIndex(outputStream.Index())
	outputPacketsCh <- packet.BuildOutput(
		pkt,
		outputStream,
		r.outputFormatContext,
	)
	return nil
}

func (r *Encoder[EF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	encoder := r.encoders[input.GetStreamIndex()]
	logger.Tracef(ctx, "encoder == %v", encoder)
	if encoder == nil {
		err := r.updateOutputs(ctx, input.FormatContext)
		if err != nil {
			return fmt.Errorf("unable to update outputs: %w", err)
		}
		encoder = r.encoders[input.GetStreamIndex()]
	}
	assert(ctx, encoder != nil)

	if encoder.Encoder == (codec.EncoderCopy{}) {
		return fmt.Errorf("one should not call SendInputFrame for the 'copy' encoder")
	}

	outputStream := r.outputStreams[input.GetStreamIndex()]
	assert(ctx, outputStream != nil)
	if enableStreamCodecParametersUpdates {
		if getInitTSer, ok := encoder.Encoder.(interface{ GetInitTS() time.Time }); ok {
			initTS := getInitTSer.GetInitTS()
			if encoder.LastInitTS.Before(initTS) {
				logger.Debugf(ctx, "updating the codec parameters")
				encoder.ToCodecParameters(outputStream.CodecParameters())
				encoder.LastInitTS = initTS
			}
		}
	}

	err := encoder.SendFrame(ctx, input.Frame)
	if err != nil {
		return fmt.Errorf("unable to send a frame to the encoder: %w", err)
	}

	for {
		pkt := packet.Pool.Get()
		err := encoder.ReceivePacket(ctx, pkt)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			logger.Debugf(ctx, "encoder.CodecContext().ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			packet.Pool.Put(pkt) // TODO: it's already unref-ed, so bypassing the reset (which would unref)
			if isEOF || isEAgain {
				break
			}
			return fmt.Errorf("unable receive the packet from the encoder: %w", err)
		}

		pkt.SetDts(input.DTS)
		pkt.SetPts(input.Pts())
		pkt.SetStreamIndex(outputStream.Index())
		outputPacketsCh <- packet.BuildOutput(
			pkt,
			outputStream,
			r.outputFormatContext,
		)
	}

	return nil
}
