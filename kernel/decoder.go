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

func (d *Decoder[DF]) GetStreamDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (*codec.Decoder, error) {
	decoder := d.decoders[stream.Index()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder != nil {
		return decoder, nil
	}
	decoder, err := d.DecoderFactory.NewDecoder(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize a decoder for stream %d: %w", stream.Index(), err)
	}
	assert(ctx, decoder != nil)
	d.decoders[stream.Index()] = decoder
	return decoder, nil
}

func (d *Decoder[DF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	if d.IsClosed() {
		return io.ErrClosedPipe
	}

	decoder, err := d.GetStreamDecoder(ctx, input.Stream)
	if err != nil {
		return fmt.Errorf("unable to get a stream decoder: %w", err)
	}

	if err := decoder.SendPacket(ctx, input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendInput(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	for {
		shouldContinue, err := func() (bool, error) {
			f := frame.Pool.Get()
			err := decoder.ReceiveFrame(ctx, f)
			if err != nil {
				frame.Pool.Pool.Put(f)
				isEOF := errors.Is(err, astiav.ErrEof)
				isEAgain := errors.Is(err, astiav.ErrEagain)
				logger.Tracef(ctx, "decoder.ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
				if isEOF || isEAgain {
					return false, nil
				}
				return false, fmt.Errorf("unable to receive a frame from the decoder: %w", err)
			}
			logger.Tracef(ctx, "decoder.ReceiveFrame(): received a frame")

			timeBase := input.Stream.TimeBase()
			if timeBase.Num() == 0 {
				return false, fmt.Errorf("internal error: TimeBase is not set")
			}
			outputFramesCh <- frame.BuildOutput(
				f,
				decoder.CodecContext(),
				input.StreamIndex(), input.FormatContext.NbStreams(),
				input.Stream.Duration(),
				timeBase,
				input.Packet.Pos(), input.Packet.Duration(),
			)
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

func (d *Decoder[DF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
}
