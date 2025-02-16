package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type InputPacket struct {
	*astiav.Packet
}

type OutputPacket struct {
	*astiav.Packet
}

func (o *OutputPacket) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}

type Recoder struct {
	locker         sync.Mutex
	decoderFactory DecoderFactory
	encoderFactory EncoderFactory
	decoders       map[int]*Decoder
	encoders       map[int]*Encoder
	frame          *astiav.Frame
	closer         astikit.Closer
	isClosed       bool
	inputChan      chan InputPacket
	outputChan     chan OutputPacket
}

var _ ProcessingNode = (*Recoder)(nil)

func NewRecoder(
	ctx context.Context,
	decoderFactory DecoderFactory,
	encoderFactory EncoderFactory,
) (*Recoder, error) {
	r := &Recoder{
		frame:          astiav.AllocFrame(),
		inputChan:      make(chan InputPacket, 100),
		outputChan:     make(chan OutputPacket, 1),
		decoderFactory: decoderFactory,
		encoderFactory: encoderFactory,
		decoders:       map[int]*Decoder{},
		encoders:       map[int]*Encoder{},
	}
	r.closer.Add(r.frame.Free)
	r.closer.Add(func() {
		close(r.outputChan)
		r.isClosed = true
	})
	observability.Go(ctx, func() {
		err := r.readerLoop(ctx)
		if err != nil {
			errmon.ObserveErrorCtx(ctx, err)
		}
	})
	return r, nil
}

func (r *Recoder) readerLoop(
	ctx context.Context,
) error {
	return readerLoop(ctx, r.inputChan, r)
}

func (r *Recoder) SendPacketChan() chan<- InputPacket {
	return r.inputChan
}

func (r *Recoder) Close() error {
	r.locker.Lock()
	defer r.locker.Unlock()
	return r.closer.Close()
}

func (r *Recoder) SendPacket(
	ctx context.Context,
	input InputPacket,
) (_err error) {
	r.locker.Lock()
	defer r.locker.Unlock()
	if r.isClosed {
		return io.ErrClosedPipe
	}

	encoder := r.encoders[input.StreamIndex()]
	if encoder == nil {
		var err error
		encoder, err = r.encoderFactory.NewEncoder(ctx, input.Packet)
		if err != nil {
			return fmt.Errorf("cannot initialize an encoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.closer.AddWithError(encoder.Close)
		r.encoders[input.StreamIndex()] = encoder
	}

	decoder := r.decoders[input.StreamIndex()]
	if decoder == nil {
		var err error
		decoder, err = r.decoderFactory.NewDecoder(ctx, input.Packet)
		if err != nil {
			return fmt.Errorf("cannot initialize a decoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.closer.AddWithError(decoder.Close)
		r.decoders[input.StreamIndex()] = decoder
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
				logger.Debugf(ctx, "decoder.CodecContext().ReceiveFrame(): %v", err)
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
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
			logger.Debugf(ctx, "encoder.CodecContext().ReceivePacket(): %v", err)
			PacketPool.Pool.Put(packet) // TODO: it's already unref-ed, so bypassing the reset (which would unref)
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return fmt.Errorf("unable receive the packet from the encoder: %w", err)
		}

		r.outputChan <- OutputPacket{
			Packet: packet,
		}
	}
	return nil
}

func (c *Recoder) OutputPacketsChan() <-chan OutputPacket {
	return c.outputChan
}
