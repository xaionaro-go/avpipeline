package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type Recoder struct {
	locker           sync.Mutex
	decoderFactory   DecoderFactory
	encoderFactory   EncoderFactory
	decoders         map[int]*Decoder
	encoders         map[int]Encoder
	frame            *astiav.Frame
	closeOnce        sync.Once
	closer           *astikit.Closer
	isClosed         bool
	inputChan        chan InputPacket
	outputChan       chan OutputPacket
	errorChan        chan error
	streamConfigurer StreamConfigurer

	outputFormatContext *astiav.FormatContext
	outputStreams       map[int]*astiav.Stream
}

var _ ProcessingNode = (*Recoder)(nil)

func NewRecoder(
	ctx context.Context,
	decoderFactory DecoderFactory,
	encoderFactory EncoderFactory,
	streamConfigurer StreamConfigurer,
) (*Recoder, error) {
	r := &Recoder{
		frame:               astiav.AllocFrame(),
		inputChan:           make(chan InputPacket, 100),
		outputChan:          make(chan OutputPacket, 1),
		errorChan:           make(chan error, 2),
		decoderFactory:      decoderFactory,
		encoderFactory:      encoderFactory,
		decoders:            map[int]*Decoder{},
		encoders:            map[int]Encoder{},
		streamConfigurer:    streamConfigurer,
		outputFormatContext: astiav.AllocFormatContext(),
		outputStreams:       make(map[int]*astiav.Stream),
		closer:              astikit.NewCloser(),
	}
	setFinalizerFree(ctx, r.frame)
	setFinalizerFree(ctx, r.outputFormatContext)

	startReaderLoop(ctx, r)
	return r, nil
}

func (r *Recoder) addToCloser(callback func()) {
	r.closer.Add(callback)
}

func (r *Recoder) outChanError() chan<- error {
	return r.errorChan
}

func (r *Recoder) readLoop(
	ctx context.Context,
) error {
	return ReaderLoop(ctx, r.inputChan, r)
}

func (r *Recoder) finalize(ctx context.Context) error {
	r.locker.Lock()
	defer r.locker.Unlock()
	logger.Debugf(ctx, "closing recoder")
	close(r.outputChan)
	r.isClosed = true
	for key, decoder := range r.decoders {
		err := decoder.Close()
		logger.Debugf(ctx, "decoder closed: %v", err)
		delete(r.decoders, key)
	}
	for key, encoder := range r.encoders {
		err := encoder.Close()
		logger.Debugf(ctx, "encoder closed: %v", err)
		delete(r.encoders, key)
	}
	return nil
}

func (r *Recoder) SendPacketChan() chan<- InputPacket {
	return r.inputChan
}

func (r *Recoder) Close() error {
	var err error
	r.closeOnce.Do(func() {
		err = r.closer.Close()
	})
	return err
}

func (r *Recoder) lazyInitOutputStreamCopy(
	ctx context.Context,
	input InputPacket,
) (_ *astiav.Stream, _err error) {
	logger.Tracef(ctx, "lazyInitOutputStreamCopy: streamIndex: %d", input.Packet.StreamIndex())
	defer func() {
		if _err == nil {
			assert(ctx, r.outputStreams[input.Packet.StreamIndex()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStreamCopy: streamIndex: %d: %v", input.Packet.StreamIndex(), _err)
	}()
	outputStream := r.outputStreams[input.Packet.StreamIndex()]
	if outputStream != nil {
		return outputStream, nil
	}
	logger.Debugf(ctx, "new output (copy) stream (stream index: %d)", input.Packet.StreamIndex())

	codecParams := input.Stream.CodecParameters()
	codec := astiav.FindDecoder(codecParams.CodecID())
	outputStream = r.outputFormatContext.NewStream(codec)
	if outputStream == nil {
		return nil, fmt.Errorf("unable to initialize an (copy) output stream")
	}
	err := codecParams.Copy(outputStream.CodecParameters())
	if err != nil {
		return nil, fmt.Errorf("unable to copy codec parameters: %w", err)
	}

	err = r.configureOutputStream(ctx, outputStream, input)
	if err != nil {
		return nil, fmt.Errorf("unable to configure the stream: %w", err)
	}

	return outputStream, nil
}

func (r *Recoder) lazyInitOutputStream(
	ctx context.Context,
	input InputPacket,
	encoder Encoder,
) (_ *astiav.Stream, _err error) {
	logger.Tracef(ctx, "lazyInitOutputStream: streamIndex: %d", input.Packet.StreamIndex())
	defer func() {
		if _err == nil {
			assert(ctx, r.outputStreams[input.Packet.StreamIndex()] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStream: streamIndex: %d: %v", input.Packet.StreamIndex(), _err)
	}()

	outputStream := r.outputStreams[input.Packet.StreamIndex()]
	if outputStream != nil {
		return outputStream, nil
	}
	logger.Debugf(ctx, "new output stream (stream index: %d)", input.Packet.StreamIndex())

	outputStream = r.outputFormatContext.NewStream(encoder.Codec())
	if outputStream == nil {
		return nil, fmt.Errorf("unable to initialize an output stream")
	}
	if err := encoder.CodecContext().ToCodecParameters(outputStream.CodecParameters()); err != nil {
		return nil, fmt.Errorf("unable to copy codec parameters from the encoder to the output stream: %w", err)
	}

	err := r.configureOutputStream(ctx, outputStream, input)
	if err != nil {
		return nil, fmt.Errorf("unable to configure the stream: %w", err)
	}

	return outputStream, nil
}

func (r *Recoder) configureOutputStream(
	ctx context.Context,
	outputStream *astiav.Stream,
	input InputPacket,
) error {
	inputStreamIndex := input.Packet.StreamIndex()
	outputStream.SetIndex(inputStreamIndex)
	copyNonCodecStreamParameters(outputStream, input.Stream)
	if r.streamConfigurer != nil {
		err := r.streamConfigurer.StreamConfigure(ctx, outputStream, input.Packet)
		if err != nil {
			return fmt.Errorf("unable to configure the output stream: %w", err)
		}
	}
	logger.Tracef(
		ctx,
		"resulting output stream for input stream %d: %d: %s: %s: %s: %s: %s",
		inputStreamIndex,
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	r.outputStreams[inputStreamIndex] = outputStream
	assert(ctx, outputStream != nil)

	return nil
}

func (r *Recoder) SendPacket(
	ctx context.Context,
	input InputPacket,
) (_err error) {
	logger.Tracef(ctx, "SendPacket")
	defer func() { logger.Tracef(ctx, "/SendPacket: %v", _err) }()
	r.locker.Lock()
	defer r.locker.Unlock()
	if r.isClosed {
		return io.ErrClosedPipe
	}

	encoder := r.encoders[input.StreamIndex()]
	logger.Tracef(ctx, "encoder == %v", encoder)
	if encoder == nil {
		var err error
		encoder, err = r.encoderFactory.NewEncoder(ctx, input)
		if err != nil {
			return fmt.Errorf("cannot initialize an encoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.encoders[input.StreamIndex()] = encoder
	}
	assert(ctx, encoder != nil)

	if encoder == (EncoderCopy{}) {
		outputStream, err := r.lazyInitOutputStreamCopy(ctx, input)
		if err != nil {
			return fmt.Errorf("unable to initialize the output stream (copy): %w", err)
		}
		pkt := ClonePacketAsReferenced(input.Packet)
		pkt.SetStreamIndex(outputStream.Index())
		r.outputChan <- OutputPacket{
			Packet: pkt,
		}
		return nil
	}

	decoder := r.decoders[input.StreamIndex()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder == nil {
		var err error
		decoder, err = r.decoderFactory.NewDecoder(ctx, input)
		if err != nil {
			return fmt.Errorf("cannot initialize a decoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.decoders[input.StreamIndex()] = decoder
	}
	assert(ctx, decoder != nil)

	outputStream, err := r.lazyInitOutputStream(ctx, input, encoder)
	if err != nil {
		return fmt.Errorf("unable to initialize the output stream: %w", err)
	}

	// TODO: investigate: Do we really need this? And why?
	//input.Packet.RescaleTs(inputStream.TimeBase(), decoder.CodecContext().TimeBase())

	if err := decoder.CodecContext().SendPacket(input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendPacket(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	frame := r.frame
	for {
		shouldContinue, err := func() (bool, error) {
			err := decoder.CodecContext().ReceiveFrame(frame)
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

			err = encoder.CodecContext().SendFrame(frame)
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
		packet := PacketPool.Get()
		err := encoder.CodecContext().ReceivePacket(packet)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			logger.Debugf(ctx, "encoder.CodecContext().ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			PacketPool.Pool.Put(packet) // TODO: it's already unref-ed, so bypassing the reset (which would unref)
			if isEOF || isEAgain {
				break
			}
			return fmt.Errorf("unable receive the packet from the encoder: %w", err)
		}

		packet.SetStreamIndex(outputStream.Index())
		func() {
			r.outputChan <- OutputPacket{
				Packet: packet,
			}
		}()
	}
	return nil
}

func (r *Recoder) OutputPacketsChan() <-chan OutputPacket {
	return r.outputChan
}

func (r *Recoder) ErrorChan() <-chan error {
	return r.errorChan
}

func (r *Recoder) GetOutputFormatContext(ctx context.Context) *astiav.FormatContext {
	return r.outputFormatContext
}

func (r *Recoder) String() string {
	return fmt.Sprintf("Recoder(%s->%s)", r.decoderFactory, r.encoderFactory)
}
