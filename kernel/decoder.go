package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Decoder[DF codec.DecoderFactory] struct {
	DecoderFactory DF

	*closeChan
	decoders map[int]*codec.Decoder
}

var _ Abstract = (*Decoder[codec.DecoderFactory])(nil)

func NewDecoder[DF codec.DecoderFactory](
	ctx context.Context,
	decoderFactory DF,
) *Decoder[DF] {
	d := &Decoder[DF]{
		DecoderFactory: decoderFactory,
		closeChan:      newCloseChan(),
		decoders:       map[int]*codec.Decoder{},
	}
	return d
}

func (d *Decoder[DF]) Close(ctx context.Context) error {
	d.closeChan.Close(ctx)
	for key, decoder := range d.decoders {
		err := decoder.Close(ctx)
		logger.Debugf(ctx, "decoder closed: %v", err)
		delete(d.decoders, key)
	}
	return nil
}

func (d *Decoder[DF]) String() string {
	return fmt.Sprintf("Decoder(%s)", d.DecoderFactory)
}

func (d *Decoder[DF]) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

func (r *Decoder[DF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	decoder := r.decoders[input.StreamIndex()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder == nil {
		var err error
		decoder, err = r.DecoderFactory.NewDecoder(ctx, input.Stream)
		if err != nil {
			return fmt.Errorf("cannot initialize a decoder for stream %d: %w", input.StreamIndex(), err)
		}
		r.decoders[input.StreamIndex()] = decoder
	}
	assert(ctx, decoder != nil)

	if err := decoder.SendPacket(ctx, input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendInput(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	// TODO: investigate: Do we really need this? And why?
	//input.Packet.RescaleTs(inputStream.TimeBase(), decoder.CodecContext().TimeBase())

	for {
		shouldContinue, err := func() (bool, error) {
			f := frame.Pool.Get()
			err := decoder.ReceiveFrame(ctx, f)
			if err != nil {
				isEOF := errors.Is(err, astiav.ErrEof)
				isEAgain := errors.Is(err, astiav.ErrEagain)
				logger.Debugf(ctx, "decoder.CodecContext().ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
				if isEOF || isEAgain {
					return false, nil
				}
				return false, fmt.Errorf("unable to receive a frame from the decoder: %w", err)
			}
			outputFramesCh <- frame.BuildOutput(f, input.Packet.Dts(), input.Stream, input.FormatContext)
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

func (r *Decoder[DF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
}
