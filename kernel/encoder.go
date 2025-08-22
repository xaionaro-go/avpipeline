package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/scaler"
	"github.com/xaionaro-go/xsync"
)

const (
	encoderWriteHeaderOnFinishedGettingStreams = false
	encoderWriteHeaderOnNotifyPacketSources    = false
	encoderCopyDTSPTS                          = true
	encoderDTSHigherPTSCorrect                 = false
	encoderDebug                               = false
)

type Encoder[EF codec.EncoderFactory] struct {
	*closuresignaler.ClosureSignaler

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
	Scaler      scaler.Scaler
	ScaledFrame *astiav.Frame
	LastInitTS  time.Time
}

func NewEncoder[EF codec.EncoderFactory](
	ctx context.Context,
	encoderFactory EF,
	streamConfigurer StreamConfigurer,
) *Encoder[EF] {
	logger.Tracef(ctx, "NewEncoder")
	defer func() { logger.Tracef(ctx, "/NewEncoder") }()
	e := &Encoder[EF]{
		ClosureSignaler:     closuresignaler.New(),
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
	e.ClosureSignaler.Close(ctx)
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
		if _err == nil && e.outputStreams[streamIndex] == nil {
			_err = fmt.Errorf("internal error: output stream for stream index %d is somehow still nil after an explicit request for initialization", streamIndex)
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
) (_err error) {
	defer func() {
		if _err == nil && e.outputStreams[streamIndex] == nil {
			_err = fmt.Errorf("internal error: output stream for stream index %d is somehow still nil after an explicit request for initialization", streamIndex)
		}
	}()
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

	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		logger.Tracef(ctx, "FPS: %v", params.FrameRate())
	case astiav.MediaTypeAudio:
		logger.Tracef(ctx, "SampleRate: %d", params.SampleRate())
	}
	err := e.initEncoderFor(ctx, streamIndex, params, timeBase)
	if err != nil {
		return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", streamIndex, err)
	}

	encoder := e.encoders[streamIndex]
	if encoder == nil {
		return fmt.Errorf("internal error: encoder for stream index %d is nil after an explicit request for initialization", streamIndex)
	}
	switch {
	case codec.IsEncoderCopy(encoder.Encoder):
		err = e.initOutputStreamCopy(ctx, streamIndex, params, timeBase)
	case codec.IsEncoderRaw(encoder.Encoder):
		return nil
	default:
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
	if !codec.IsFakeEncoder(encoderInstance) && encoderInstance.CodecContext() == nil {
		return fmt.Errorf("the encoder factory produced an encoder %T with nil CodecContext", encoderInstance)
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
	logger.Tracef(ctx, "SendInputPacket: %s", input.GetMediaType())
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %s: %v", input.GetMediaType(), _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}
	return xsync.DoA4R1(xsync.WithNoLogging(ctx, true), &e.Locker, e.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func fixCodecParameters(
	ctx context.Context,
	orig *astiav.CodecParameters,
	averageFPS astiav.Rational,
) *astiav.CodecParameters {
	cp := astiav.AllocCodecParameters()
	setFinalizerFree(ctx, cp)
	orig.Copy(cp)
	if cp.FrameRate().Float64() < 1 {
		if averageFPS.Float64() < 1 {
			averageFPS = astiav.NewRational(30, 1)
		}
		logger.Errorf(ctx, "unable to detect the FPS, correcting to %s", averageFPS)
		cp.SetFrameRate(averageFPS)
	}
	return cp
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
		input.Stream.AvgFrameRate()
		logger.Debugf(ctx, "an encoder is not initialized, yet")
		codecParams := fixCodecParameters(ctx, input.CodecParameters(), input.Stream.AvgFrameRate())
		err := e.initEncoderAndOutputFor(ctx, input.GetStreamIndex(), codecParams, input.GetStream().TimeBase())
		if err != nil {
			return fmt.Errorf("unable to update outputs (packet): %w", err)
		}
		encoder = e.encoders[input.GetStreamIndex()]
	}
	assert(ctx, encoder != nil)
	ctx = belt.WithField(ctx, "encoder", encoder)

	if !codec.IsEncoderCopy(encoder.Encoder) {
		return ErrNotCopyEncoder{}
	}

	outputStream := e.outputStreams[input.GetStreamIndex()]
	assert(ctx, outputStream != nil)
	assert(ctx, outputStream.CodecParameters().MediaType() == input.GetMediaType(), outputStream.CodecParameters().MediaType(), input.GetMediaType())
	pkt := packet.CloneAsReferenced(input.Packet)
	pkt.SetStreamIndex(outputStream.Index())
	if err := e.send(ctx, pkt, input.PipelineSideData, outputStream, outputPacketsCh); err != nil {
		return fmt.Errorf("unable to send a packet: %w", err)
	}
	return nil
}

func (e *Encoder[EF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}

	return xsync.DoA4R1(xsync.WithNoLogging(ctx, true), &e.Locker, e.sendInputFrame, ctx, input, outputPacketsCh, outputFramesCh)
}

func (e *Encoder[EF]) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	encoder := e.encoders[input.GetStreamIndex()]
	logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), encoder)
	if encoder == nil {
		logger.Debugf(ctx, "an encoder is not initialized, yet")
		codecParams := fixCodecParameters(ctx, input.CodecParameters, input.AvgFrameRate)
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
	ctx = belt.WithField(ctx, "encoder", encoder)

	if encoderWriteHeaderOnFinishedGettingStreams && !e.headerIsWritten && len(e.encoders) == input.StreamsCount {
		logger.Debugf(ctx, "writing the header")
		err := e.outputFormatContext.WriteHeader(nil)
		if err != nil {
			return fmt.Errorf("unable to write header: %w", err)
		}
		e.headerIsWritten = true
	}

	if codec.IsEncoderRaw(encoder.Encoder) {
		outputFramesCh <- frame.BuildOutput(
			frame.CloneAsReferenced(input.Frame),
			input.Pos,
			input.StreamInfo,
		)
		return nil
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

	if encoderDebug {
		if encoder, ok := encoder.Encoder.(interface {
			SanityCheck(ctx context.Context) error
		}); ok {
			if err := encoder.SanityCheck(ctx); err != nil {
				return fmt.Errorf("encoder sanity check failed: %w", err)
			}
		}
	}

	outputMediaType := outputStream.CodecParameters().MediaType()
	encoderMediaType := encoder.MediaType()
	assert(ctx, outputMediaType == encoderMediaType, outputMediaType, encoderMediaType)

	scaledFrame := input.Frame
	width, height := encoder.GetResolution(ctx)
	if input.Frame.Width() != int(width) || input.Frame.Height() != int(height) {
		var err error
		scaledFrame, err = encoder.getScaledFrame(ctx, input.Frame, codec.Resolution{
			Width:  width,
			Height: height,
		})
		if err != nil {
			return fmt.Errorf("unable to scale the frame: %w", err)
		}
	}

	err := encoder.SendFrame(ctx, scaledFrame)
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
		if encoderCopyDTSPTS {
			pkt.SetDts(input.PktDts())
			pkt.SetPts(input.Pts())
			pkt.RescaleTs(input.TimeBase, outputStream.TimeBase())
		}
		//pkt.SetPos(-1) // <- TODO: should this happen? why?
		if pkt.Dts() > pkt.Pts() && pkt.Dts() != consts.NoPTSValue && pkt.Pts() != consts.NoPTSValue {
			if encoderDTSHigherPTSCorrect {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) correcting DTS to %d", pkt.Dts(), pkt.Pts(), pkt.Pts())
				pkt.SetDts(pkt.Pts())
			} else {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) skipping the packet", pkt.Dts(), pkt.Pts())
				packet.Pool.Put(pkt)
				continue
			}
		}

		if err := e.send(ctx, pkt, input.PipelineSideData, outputStream, outputPacketsCh); err != nil {
			return fmt.Errorf("unable to send a packet: %w", err)
		}
	}

	return nil
}

func (e *streamEncoder) getScaledFrame(
	ctx context.Context,
	inputFrame *astiav.Frame,
	resolution codec.Resolution,
) (_ret *astiav.Frame, _err error) {
	logger.Tracef(ctx, "getScaledFrame: %dx%d", resolution.Width, resolution.Height)
	defer func() { logger.Tracef(ctx, "/getScaledFrame: %dx%d: %v", resolution.Width, resolution.Height, _err) }()

	err := e.prepareScaler(ctx, inputFrame, resolution)
	if err != nil {
		return nil, fmt.Errorf("unable to get a scaler: %w", err)
	}

	e.Scaler.ScaleFrame(ctx, inputFrame, e.ScaledFrame)
	return e.ScaledFrame, nil
}

func getResolutionFromAstiavFrame(
	frame *astiav.Frame,
) codec.Resolution {
	if frame == nil {
		return codec.Resolution{}
	}
	return codec.Resolution{
		Width:  uint32(frame.Width()),
		Height: uint32(frame.Height()),
	}
}

func (e *streamEncoder) prepareScaler(
	ctx context.Context,
	inputFrame *astiav.Frame,
	outputResolution codec.Resolution,
) (_err error) {
	logger.Tracef(ctx, "getScaler: %dx%d", outputResolution.Width, outputResolution.Height)
	defer func() {
		logger.Tracef(ctx, "/getScaler: %dx%d: %v", outputResolution.Width, outputResolution.Height, _err)
	}()

	inputResolution := getResolutionFromAstiavFrame(inputFrame)
	if e.Scaler != nil {
		if e.Scaler.SourceResolution() == inputResolution && e.Scaler.DestinationResolution() == outputResolution {
			logger.Tracef(ctx, "reusing the scaler")
			return nil
		}
		if err := e.Scaler.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close the scaler: %v", err)
		}
	}

	e.ScaledFrame = astiav.AllocFrame()
	setFinalizerFree(ctx, e.ScaledFrame)
	e.ScaledFrame.SetWidth(int(outputResolution.Width))
	e.ScaledFrame.SetHeight(int(outputResolution.Height))
	e.ScaledFrame.SetPixelFormat(inputFrame.PixelFormat())
	if err := e.ScaledFrame.AllocBuffer(0); err != nil { // TODO(memleak): make sure the buffer is not already allocated, otherwise it may lead to a memleak
		return fmt.Errorf("unable to allocate a buffer for the scaled frame: %w", err)
	}

	s, err := scaler.NewSoftware(
		ctx,
		inputResolution, inputFrame.PixelFormat(),
		outputResolution, e.ScaledFrame.PixelFormat(),
		astiav.SoftwareScaleContextFlagLanczos,
	)
	if err != nil {
		return fmt.Errorf("unable to create a scaler: %w", err)
	}

	e.Scaler = s
	return nil
}

func (e *Encoder[EF]) send(
	ctx context.Context,
	outPkt *astiav.Packet,
	pipelineSideData []any,
	outputStream *astiav.Stream,
	out chan<- packet.Output,
) (_err error) {

	outPktWrapped := packet.BuildOutput(
		outPkt,
		packet.BuildStreamInfo(
			outputStream,
			e,
			pipelineSideData,
		),
	)

	logger.Tracef(ctx, "sending out %s: dts:%d; pts:%d", outPktWrapped.CodecParameters().MediaType(), outPktWrapped.Dts(), outPktWrapped.Pts())
	defer func() { logger.Tracef(ctx, "/send: %v %v", outPktWrapped.CodecParameters().MediaType(), _err) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- outPktWrapped:
		return nil
	}
}

func (e *Encoder[EF]) WithOutputFormatContext(
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
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		e.Locker.Do(ctx, func() {
			changed := false
			for _, inputStream := range fmtCtx.Streams() {
				if e.encoders[inputStream.Index()] != nil {
					continue
				}
				changed = true
				codecParams := fixCodecParameters(ctx, inputStream.CodecParameters(), inputStream.AvgFrameRate())
				err := e.initEncoderAndOutputFor(
					ctx,
					inputStream.Index(),
					codecParams,
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

func (e *Encoder[EF]) Reset(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &e.Locker, e.reset, ctx)
}

func (e *Encoder[EF]) reset(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()

	var errs []error
	for streamIndex, encoder := range e.encoders {
		if err := encoder.Reset(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset the encoder for stream #%d: %w", streamIndex, err))
		}
	}

	return errors.Join(errs...)
}
