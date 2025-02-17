package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type InputPacket struct {
	*astiav.Packet
	*astiav.Stream
	*astiav.FormatContext
}

type OutputPacket struct {
	*astiav.Packet
}

func (o *OutputPacket) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}

type StreamConfigurer interface {
	StreamConfigure(ctx context.Context, stream *astiav.Stream, pkt *astiav.Packet) error
}

type FrameSender interface {
	SendFrame(ctx context.Context, frame *Frame) error
}

type FrameReader struct {
	locker         sync.Mutex
	decoderFactory DecoderFactory
	decoders       map[int]*Decoder
	frame          *astiav.Frame
	closer         *astikit.Closer
	isClosed       bool
	inputChan      chan InputPacket
	errorChan      chan error
	frameSender    FrameSender

	formatContext *astiav.FormatContext
}

var _ ProcessingNode = (*FrameReader)(nil)

func NewFrameReader(
	ctx context.Context,
	decoderFactory DecoderFactory,
	frameSender FrameSender,
) (*FrameReader, error) {
	r := &FrameReader{
		frame:          astiav.AllocFrame(),
		inputChan:      make(chan InputPacket, 100),
		errorChan:      make(chan error, 1),
		decoderFactory: decoderFactory,
		decoders:       map[int]*Decoder{},
		formatContext:  astiav.AllocFormatContext(),
		frameSender:    frameSender,
		closer:         astikit.NewCloser(),
	}
	r.closer.Add(r.frame.Free)
	r.closer.Add(r.formatContext.Free)
	observability.Go(ctx, func() {
		r.errorChan <- r.readerLoop(ctx)
	})
	return r, nil
}

func (r *FrameReader) readerLoop(
	ctx context.Context,
) error {
	return ReaderLoop(ctx, r.inputChan, r)
}

func (r *FrameReader) SendPacketChan() chan<- InputPacket {
	return r.inputChan
}

func (r *FrameReader) Close() error {
	r.locker.Lock()
	defer r.locker.Unlock()
	return r.closer.Close()
}

func (r *FrameReader) SendPacket(
	ctx context.Context,
	input InputPacket,
) (_err error) {
	r.locker.Lock()
	defer r.locker.Unlock()
	if r.isClosed {
		return io.ErrClosedPipe
	}

	decoder := r.decoders[input.StreamIndex()]
	if decoder == nil {
		var err error
		decoder, err = r.decoderFactory.NewDecoder(ctx, input)
		if err != nil {
			return fmt.Errorf("cannot initialize a decoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.closer.AddWithError(decoder.Close)
		r.decoders[input.StreamIndex()] = decoder
	}
	assert(ctx, decoder != nil)

	// TODO: investigate: Do we really need this? And why?
	//input.Packet.RescaleTs(inputStream.TimeBase(), decoder.CodecContext().TimeBase())

	if err := decoder.CodecContext().SendPacket(input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendPacket(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	frameBuf := r.frame
	sendFrame := &Frame{
		Frame:       frameBuf,
		InputPacket: &input,
		Decoder:     decoder,
	}
	for {
		shouldContinue, err := func() (bool, error) {
			err := decoder.CodecContext().ReceiveFrame(frameBuf)
			if err != nil {
				logger.Debugf(ctx, "decoder.CodecContext().ReceiveFrame(): %v", err)
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					return false, nil
				}
				return false, fmt.Errorf("unable to receive a frame from the decoder: %w", err)
			}
			defer frameBuf.Unref()

			err = r.frameSender.SendFrame(ctx, sendFrame)
			if err != nil {
				return false, fmt.Errorf("unable to send a frame: %w", err)
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

	return nil
}

func (c *FrameReader) OutputPacketsChan() <-chan OutputPacket {
	return nil
}

func (c *FrameReader) ErrorChan() <-chan error {
	return c.errorChan
}

func (c *FrameReader) GetOutputFormatContext(ctx context.Context) *astiav.FormatContext {
	return nil
}
