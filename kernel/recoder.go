package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/stream"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

const (
	enableStreamCodecParametersUpdates = false
)

type Recoder struct {
	*closeChan

	decoderFactory   codec.DecoderFactory
	encoderFactory   codec.EncoderFactory
	decoders         map[int]*codec.Decoder
	encoders         map[int]*encoderInRecoder
	frame            *astiav.Frame
	closeOnce        sync.Once
	closer           *astikit.Closer
	streamConfigurer StreamConfigurer

	outputFormatContextLocker xsync.RWMutex
	outputFormatContext       *astiav.FormatContext
	outputStreams             map[int]*astiav.Stream
}

var _ Abstract = (*Recoder)(nil)

func NewRecoder(
	ctx context.Context,
	decoderFactory codec.DecoderFactory,
	encoderFactory codec.EncoderFactory,
	streamConfigurer StreamConfigurer,
) (*Recoder, error) {
	r := &Recoder{
		frame:               astiav.AllocFrame(),
		decoderFactory:      decoderFactory,
		encoderFactory:      encoderFactory,
		decoders:            map[int]*codec.Decoder{},
		encoders:            map[int]*encoderInRecoder{},
		streamConfigurer:    streamConfigurer,
		closeChan:           newCloseChan(),
		outputFormatContext: astiav.AllocFormatContext(),
		outputStreams:       make(map[int]*astiav.Stream),
		closer:              astikit.NewCloser(),
	}
	setFinalizerFree(ctx, r.frame)
	setFinalizerFree(ctx, r.outputFormatContext)
	return r, nil
}
func (r *Recoder) Close(ctx context.Context) error {
	r.closeChan.Close()
	for key, decoder := range r.decoders {
		err := decoder.Close(ctx)
		logger.Debugf(ctx, "decoder closed: %v", err)
		delete(r.decoders, key)
	}
	for key, encoder := range r.encoders {
		err := encoder.Close(ctx)
		logger.Debugf(ctx, "encoder closed: %v", err)
		delete(r.encoders, key)
	}
	return nil
}

func (r *Recoder) initOutputStreamCopy(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStreamCopy: streamIndex: %d", inputStream.Index())
	defer func() {
		if _err == nil {
			assert(ctx, r.outputStreams[inputStream.Index()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStreamCopy: streamIndex: %d: %v", inputStream.Index(), _err)
	}()
	logger.Debugf(ctx, "new output (copy) stream (stream index: %d)", inputStream.Index())

	codecParams := inputStream.CodecParameters()
	codec := astiav.FindDecoder(codecParams.CodecID())
	var outputStream *astiav.Stream
	r.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = r.outputFormatContext.NewStream(codec)
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an (copy) output stream")
	}
	err := codecParams.Copy(outputStream.CodecParameters())
	if err != nil {
		return fmt.Errorf("unable to copy codec parameters: %w", err)
	}

	err = r.configureOutputStream(ctx, outputStream, inputStream)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (r *Recoder) initOutputStream(
	ctx context.Context,
	inputStream *astiav.Stream,
	encoder codec.Encoder,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStream: streamIndex: %d", inputStream.Index())
	defer func() {
		if _err == nil {
			assert(ctx, r.outputStreams[inputStream.Index()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStream: streamIndex: %d: %v", inputStream.Index(), _err)
	}()

	logger.Debugf(ctx, "new output stream (stream index: %d)", inputStream.Index())

	var outputStream *astiav.Stream
	r.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = r.outputFormatContext.NewStream(encoder.Codec())
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an output stream")
	}
	if err := encoder.ToCodecParameters(outputStream.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy codec parameters from the encoder to the output stream: %w", err)
	}

	err := r.configureOutputStream(ctx, outputStream, inputStream)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (r *Recoder) configureOutputStream(
	ctx context.Context,
	outputStream *astiav.Stream,
	inputStream *astiav.Stream,
) error {
	outputStream.SetIndex(inputStream.Index())
	stream.CopyNonCodecParameters(outputStream, inputStream)
	if r.streamConfigurer != nil {
		err := r.streamConfigurer.StreamConfigure(ctx, outputStream, inputStream)
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
	r.outputStreams[inputStream.Index()] = outputStream
	assert(ctx, outputStream != nil)

	return nil
}

func (r *Recoder) Generate(ctx context.Context, outputCh chan<- types.OutputPacket) error {
	return nil
}

type encoderInRecoder struct {
	codec.Encoder
	LastInitTS time.Time
}

func (r *Recoder) updateOutputs(
	ctx context.Context,
	inputFmt *astiav.FormatContext,
) (_err error) {
	logger.Debugf(ctx, "updateEncoders()")
	defer func() { logger.Debugf(ctx, "/updateEncoders(): %v", _err) }()
	for _, inputStream := range inputFmt.Streams() {
		inputStreamIndex := inputStream.Index()
		if _, ok := r.encoders[inputStreamIndex]; ok {
			logger.Tracef(ctx, "stream #%d already exists, not initializing", inputStreamIndex)
			continue
		}

		err := r.initEncoderFor(ctx, inputStream)
		if err != nil {
			return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", inputStreamIndex, err)
		}

		encoder := r.encoders[inputStreamIndex]
		if encoder.Encoder == (codec.EncoderCopy{}) {
			err = r.initOutputStreamCopy(ctx, inputStream)
		} else {
			err = r.initOutputStream(ctx, inputStream, encoder)
		}
		if err != nil {
			return fmt.Errorf("unable to init an output stream for encoder %s for input stream #%d: %w", encoder, inputStreamIndex, err)
		}
	}

	return nil
}

func (r *Recoder) initEncoderFor(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	logger.Debugf(ctx, "initEncoderFor(ctx, stream[%d])", inputStream.Index())
	defer func() { logger.Debugf(ctx, "/initEncoderFor(ctx, stream[%d]): %v", inputStream.Index(), _err) }()

	encoderInstance, err := r.encoderFactory.NewEncoder(ctx, inputStream)
	if err != nil {
		return fmt.Errorf("cannot initialize an encoder for stream %d: %w", inputStream.Index(), err)
	}

	encoder := &encoderInRecoder{Encoder: encoderInstance}
	r.encoders[inputStream.Index()] = encoder
	return nil
}

func (r *Recoder) SendInput(
	ctx context.Context,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	encoder := r.encoders[input.StreamIndex()]
	logger.Tracef(ctx, "encoder == %v", encoder)
	if encoder == nil {
		err := r.updateOutputs(ctx, input.FormatContext)
		if err != nil {
			return fmt.Errorf("unable to update outputs: %w", err)
		}
		encoder = r.encoders[input.StreamIndex()]
	}
	assert(ctx, encoder != nil)

	if encoder.Encoder == (codec.EncoderCopy{}) {
		outputStream := r.outputStreams[input.Packet.StreamIndex()]
		assert(ctx, outputStream != nil)
		pkt := packet.CloneAsReferenced(input.Packet)
		pkt.SetStreamIndex(outputStream.Index())
		outputCh <- types.BuildOutputPacket(
			pkt,
			r.outputFormatContext,
		)
		return nil
	}

	decoder := r.decoders[input.StreamIndex()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder == nil {
		var err error
		decoder, err = r.decoderFactory.NewDecoder(ctx, input.Stream)
		if err != nil {
			return fmt.Errorf("cannot initialize a decoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.decoders[input.StreamIndex()] = decoder
	}
	assert(ctx, decoder != nil)

	outputStream := r.outputStreams[input.Packet.StreamIndex()]
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

	// TODO: investigate: Do we really need this? And why?
	//input.Packet.RescaleTs(inputStream.TimeBase(), decoder.CodecContext().TimeBase())

	if err := decoder.SendPacket(ctx, input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendInput(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	frame := r.frame
	for {
		shouldContinue, err := func() (bool, error) {
			err := decoder.ReceiveFrame(ctx, frame)
			if err != nil {
				isEOF := errors.Is(err, astiav.ErrEof)
				isEAgain := errors.Is(err, astiav.ErrEagain)
				logger.Debugf(ctx, "decoder.CodecContext().ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
				if isEOF || isEAgain {
					return false, nil
				}
				return false, fmt.Errorf("unable to receive a frame from the decoder: %w", err)
			}
			defer frame.Unref()

			err = encoder.SendFrame(ctx, frame)
			if err != nil {
				logger.Debugf(ctx, "encoder.CodecContext().SendFrame(): %v", err)
				return false, fmt.Errorf("unable to send the frame to the encoder: %w", err)
			}

			return true, nil
		}()
		if err != nil {
			return err
		}
		if !shouldContinue {
			break
		}
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

		pkt.SetStreamIndex(outputStream.Index())
		outputCh <- types.BuildOutputPacket(
			pkt,
			r.outputFormatContext,
		)
	}
	return nil
}

func (r *Recoder) String() string {
	return fmt.Sprintf("Recoder(%s->%s)", r.decoderFactory, r.encoderFactory)
}
