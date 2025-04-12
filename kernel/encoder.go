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
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

const (
	encoderWriteHeaderOnFinishedGettingStreams = false
	encoderWriteHeaderOnNotifyPacketSources    = false
)

type Encoder[EF codec.EncoderFactory] struct {
	*closeChan
	EncoderFactory EF
	Locker         xsync.Mutex
	PTSDurDiff     *time.Duration

	encoders                  map[int]*streamEncoder
	outputStreams             map[int]*astiav.Stream
	StreamConfigurer          StreamConfigurer
	outputFormatContextLocker xsync.RWMutex
	outputFormatContext       *astiav.FormatContext
	headerIsWritten           bool
}

var _ Abstract = (*Encoder[codec.EncoderFactory])(nil)
var _ packet.Source = (*Encoder[codec.EncoderFactory])(nil)

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
		StreamConfigurer:    streamConfigurer,
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
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStreamCopy: streamIndex: %d", streamIndex)
	defer func() {
		if _err == nil {
			assert(ctx, e.outputStreams[streamIndex] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStreamCopy: streamIndex: %d: %v", streamIndex, _err)
	}()
	logger.Debugf(ctx, "new output (copy) stream (stream index: %d)", streamIndex)

	codec := astiav.FindDecoder(params.CodecID())
	var outputStream *astiav.Stream
	e.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = e.outputFormatContext.NewStream(codec)
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an (copy) output stream")
	}
	err := params.Copy(outputStream.CodecParameters())
	if err != nil {
		return fmt.Errorf("unable to copy codec parameters: %w", err)
	}

	err = e.configureOutputStream(ctx, outputStream, streamIndex, timeBase)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) initOutputStream(
	ctx context.Context,
	streamIndex int,
	encoder codec.Encoder,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStream: streamIndex: %d", streamIndex)
	defer func() {
		if _err == nil {
			assert(ctx, e.outputStreams[streamIndex] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStream: streamIndex: %d: %v", streamIndex, _err)
	}()

	logger.Debugf(ctx, "new output stream (stream index: %d)", streamIndex)

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

	err := e.configureOutputStream(ctx, outputStream, streamIndex, encoder.CodecContext().TimeBase())
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) configureOutputStream(
	ctx context.Context,
	outputStream *astiav.Stream,
	streamIndex int,
	timeBase astiav.Rational,
) error {
	outputStream.SetIndex(streamIndex)
	outputStream.SetTimeBase(timeBase)
	if e.StreamConfigurer != nil {
		err := e.StreamConfigurer.StreamConfigure(ctx, outputStream, streamIndex)
		if err != nil {
			return fmt.Errorf("unable to configure the output stream: %w", err)
		}
	}
	logger.Debugf(
		ctx,
		"resulting output stream for input stream %d: %d: %s: %s: %s: %s: %s",
		streamIndex,
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	e.outputStreams[streamIndex] = outputStream
	assert(ctx, outputStream != nil)

	return nil
}

func (e *Encoder[EF]) initEncoderAndOutputFor(
	ctx context.Context,
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
) (_err error) {
	logger.Debugf(ctx, "initEncoderAndOutputFor(%d)", streamIndex)
	defer func() { logger.Debugf(ctx, "/initEncoderAndOutputFor(%d): %v", streamIndex, _err) }()
	if _, ok := e.encoders[streamIndex]; ok {
		logger.Errorf(ctx, "stream #%d already exists, not initializing", streamIndex)
		return nil
	}

	err := e.initEncoderFor(ctx, streamIndex, params, timeBase)
	if err != nil {
		return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", streamIndex, err)
	}

	encoder := e.encoders[streamIndex]
	if encoder.Encoder == (codec.EncoderCopy{}) {
		err = e.initOutputStreamCopy(ctx, streamIndex, params, timeBase)
	} else {
		err = e.initOutputStream(ctx, streamIndex, encoder)
	}
	if err != nil {
		return fmt.Errorf("unable to init an output stream for encoder %s for input stream #%d: %w", encoder, streamIndex, err)
	}

	return nil
}

func (e *Encoder[EF]) initEncoderFor(
	ctx context.Context,
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
) (_err error) {
	logger.Debugf(ctx, "initEncoderFor(ctx, stream[%d])", streamIndex)
	defer func() { logger.Debugf(ctx, "/initEncoderFor(ctx, stream[%d]): %v", streamIndex, _err) }()

	if timeBase.Num() == 0 {
		return fmt.Errorf("TimeBase must be set")
	}
	encoderInstance, err := e.EncoderFactory.NewEncoder(ctx, params, timeBase)
	if err != nil {
		return fmt.Errorf("cannot initialize an encoder for stream %d: %w", streamIndex, err)
	}

	encoder := &streamEncoder{Encoder: encoderInstance}
	e.encoders[streamIndex] = encoder
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

func (e *Encoder[EF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}
	return xsync.DoA4R1(xsync.WithNoLogging(ctx, true), &e.Locker, e.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func (e *Encoder[EF]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	encoder := e.encoders[input.GetStreamIndex()]
	logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), encoder)
	if encoder == nil {
		err := e.initEncoderAndOutputFor(ctx, input.GetStreamIndex(), input.CodecParameters(), input.GetStream().TimeBase())
		if err != nil {
			return fmt.Errorf("unable to update outputs (packet): %w", err)
		}
		encoder = e.encoders[input.GetStreamIndex()]
	}
	assert(ctx, encoder != nil)

	if encoder.Encoder != (codec.EncoderCopy{}) {
		return ErrNotCopyEncoder{}
	}

	outputStream := e.outputStreams[input.GetStreamIndex()]
	assert(ctx, outputStream != nil)
	assert(ctx, outputStream.CodecParameters().MediaType() == input.GetMediaType(), outputStream.CodecParameters().MediaType(), input.GetMediaType())
	pkt := packet.CloneAsReferenced(input.Packet)
	pkt.SetStreamIndex(outputStream.Index())
	e.send(ctx, pkt, outputStream, outputPacketsCh)
	return nil
}

func (e *Encoder[EF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}

	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.Locker, e.sendInputFrame, ctx, input, outputPacketsCh)
}

func (e *Encoder[EF]) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
) (_err error) {
	encoder := e.encoders[input.GetStreamIndex()]
	logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), encoder)
	if encoder == nil {
		codecParams := astiav.AllocCodecParameters()
		setFinalizerFree(ctx, codecParams)
		input.CodecContext.ToCodecParameters(codecParams)
		err := e.initEncoderAndOutputFor(ctx, input.StreamIndex, codecParams, input.GetTimeBase())
		if err != nil {
			return fmt.Errorf("unable to update outputs (frame): %w", err)
		}
		encoder = e.encoders[input.GetStreamIndex()]
		if encoderWriteHeaderOnFinishedGettingStreams && len(e.encoders) == input.StreamsCount {
			logger.Debugf(ctx, "writing the header")
			err := e.outputFormatContext.WriteHeader(nil)
			if err != nil {
				return fmt.Errorf("unable to write header: %w", err)
			}
			e.headerIsWritten = true
		}
	}
	assert(ctx, encoder != nil)

	if encoderWriteHeaderOnFinishedGettingStreams && !e.headerIsWritten && len(e.encoders) == input.StreamsCount {
		logger.Debugf(ctx, "writing the header")
		err := e.outputFormatContext.WriteHeader(nil)
		if err != nil {
			return fmt.Errorf("unable to write header: %w", err)
		}
		e.headerIsWritten = true
	}

	if encoder.Encoder == (codec.EncoderCopy{}) {
		return fmt.Errorf("one should not call SendInputFrame for the 'copy' encoder")
	}

	outputStream := e.outputStreams[input.GetStreamIndex()]
	assert(ctx, outputStream != nil, "outputStream != nil")
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

	assert(ctx, outputStream.CodecParameters().MediaType() == encoder.CodecContext().MediaType(), outputStream.CodecParameters().MediaType(), encoder.CodecContext().MediaType())

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
			logger.Tracef(ctx, "encoder.ReceivePacket(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			packet.Pool.Pool.Put(pkt)
			if isEOF || isEAgain {
				break
			}
			return fmt.Errorf("unable receive the packet from the encoder: %w", err)
		}
		logger.Tracef(ctx, "encoder.ReceivePacket(): got a %s packet, resulting size: %d (pts: %d)", outputStream.CodecParameters().MediaType(), pkt.Size(), pkt.Pts())

		pkt.SetStreamIndex(outputStream.Index())
		pkt.RescaleTs(input.GetTimeBase(), outputStream.TimeBase())
		//pkt.SetPos(-1) // <- TODO: should this happen? why?
		if pkt.Dts() > pkt.Pts() && pkt.Dts() != consts.NoPTSValue && pkt.Pts() != consts.NoPTSValue {
			logger.Errorf(ctx, "DTS (%d) > PTS (%d); as a correction setting the DTS value to be the no-PTS magic value", pkt.Dts(), pkt.Pts())
			pkt.SetDts(consts.NoPTSValue)
		}

		e.send(ctx, pkt, outputStream, outputPacketsCh)
	}

	return nil
}

func (e *Encoder[EF]) send(
	ctx context.Context,
	outPkt *astiav.Packet,
	outputStream *astiav.Stream,
	out chan<- packet.Output,
) {
	outPktWrapped := packet.BuildOutput(
		outPkt,
		outputStream,
		e,
	)

	logger.Tracef(ctx, "sending out %s", outPktWrapped.CodecParameters().MediaType())
	out <- outPktWrapped
}

func (e *Encoder[EF]) WithFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	e.Locker.Do(ctx, func() {
		callback(e.outputFormatContext)
	})
}

func (e *Encoder[EF]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	var errs []error
	source.WithFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		e.Locker.Do(ctx, func() {
			changed := false
			for _, inputStream := range fmtCtx.Streams() {
				if e.encoders[inputStream.Index()] != nil {
					continue
				}
				changed = true
				err := e.initEncoderAndOutputFor(
					ctx,
					inputStream.Index(),
					inputStream.CodecParameters(),
					inputStream.TimeBase(),
				)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream for input stream %d from source %s: %w", inputStream.Index(), source, err))
				}
			}

			if encoderWriteHeaderOnNotifyPacketSources && changed {
				logger.Debugf(ctx, "writing the header")
				err := e.outputFormatContext.WriteHeader(nil)
				if err == nil {
					e.headerIsWritten = true
				} else {
					errs = append(errs, fmt.Errorf("unable to write header: %w", err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
