package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
)

const (
	enableStreamCodecParametersUpdates = false
)

// See also https://github.com/namndev/FFmpegTutorial/blob/master/learn-ffmpeg-libav-the-hard-way.md
type Recoder[DF codec.DecoderFactory, EF codec.EncoderFactory] struct {
	*Decoder[DF]
	*Encoder[EF]
	*closeChan
}

var _ Abstract = (*Recoder[codec.DecoderFactory, codec.EncoderFactory])(nil)

func NewRecoder[DF codec.DecoderFactory, EF codec.EncoderFactory](
	ctx context.Context,
	decoderFactory DF,
	encoderFactory EF,
	streamConfigurer StreamConfigurer,
) (*Recoder[DF, EF], error) {
	r := &Recoder[DF, EF]{
		closeChan: newCloseChan(),
		Decoder:   NewDecoder(ctx, decoderFactory),
		Encoder:   NewEncoder(ctx, encoderFactory, streamConfigurer),
	}
	return r, nil
}

func (r *Recoder[DF, EF]) Close(ctx context.Context) error {
	r.closeChan.Close(ctx)
	r.Decoder.closeChan.Close(ctx)
	r.Encoder.closeChan.Close(ctx)
	return nil
}

func (r *Recoder[DF, EF]) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

func (r *Recoder[DF, EF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	err := r.Encoder.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrNotCopyEncoder{}):
	default:
		return fmt.Errorf("unable to encode the packet: %w", err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	var wg sync.WaitGroup
	defer wg.Wait()

	framesCh := make(chan frame.Output, 2)
	wg.Add(1)
	var encoderError error
	observability.Go(ctx, func() {
		defer wg.Done()
		defer cancelFn()
		for {
			f, ok := <-framesCh
			if !ok {
				return
			}
			func() {
				defer frame.Pool.Put(f.Frame)
				if encoderError != nil {
					return
				}

				err := r.Encoder.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
				if err != nil {
					encoderError = err
				}
			}()
		}
	})

	err = r.Decoder.SendInputPacket(ctx, input, outputPacketsCh, framesCh)
	close(framesCh)
	wg.Wait()
	if encoderError != nil {
		return fmt.Errorf("got an error from the encoder: %w", encoderError)
	}
	if err != nil {
		return fmt.Errorf("decoder returned an error: %w", err)
	}

	return nil
}

func (r *Recoder[DF, EF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("not implemented, yet")
}

func (r *Recoder[DF, EF]) String() string {
	return fmt.Sprintf("Recoder(%s->%s)", r.DecoderFactory, r.EncoderFactory)
}
