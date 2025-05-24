package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	enableStreamCodecParametersUpdates = false
)

// See also https://github.com/namndev/FFmpegTutorial/blob/master/learn-ffmpeg-libav-the-hard-way.md
// Note: Recoder is a somewhat hacky thing, try to not use it. Pipelining
// should be handled by pipeline, not by a Kernel. Use separately Decoder and Encoder, instead.
type Recoder[DF codec.DecoderFactory, EF codec.EncoderFactory] struct {
	*Decoder[DF]
	*Encoder[EF]
	*closeChan

	locker             xsync.Mutex
	started            bool
	activeStreamsMap   map[int]struct{}
	activeStreamsCount uint
	pendingPackets     []packet.Output
}

var _ Abstract = (*Recoder[codec.DecoderFactory, codec.EncoderFactory])(nil)
var _ packet.Source = (*Recoder[codec.DecoderFactory, codec.EncoderFactory])(nil)
var _ packet.Sink = (*Recoder[codec.DecoderFactory, codec.EncoderFactory])(nil)

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

		activeStreamsMap: make(map[int]struct{}),
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
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket")
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	return xsync.DoA3R1(ctx, &r.locker, r.sendInputPacket, ctx, input, outputPacketsCh)
}

func (r *Recoder[DF, EF]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketCh chan<- packet.Output,
) (_err error) {
	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	if r.started {
		return r.process(ctx, input, outputPacketCh)
	}

	resultCh := make(chan packet.Output, 1)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		for pkt := range resultCh {
			r.pendingPackets = append(r.pendingPackets, pkt)
			if len(r.pendingPackets) > pendingPacketsLimit {
				logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
				r.pendingPackets = r.pendingPackets[1:]
			}
			streamIdx := pkt.Stream.Index()
			if _, ok := r.activeStreamsMap[streamIdx]; ok {
				continue
			}
			r.activeStreamsCount++
			r.activeStreamsMap[streamIdx] = struct{}{}
		}
	})

	err := r.process(ctx, input, resultCh)
	close(resultCh)

	inputStreamsCount := sourceNbStreams(ctx, input.Source)
	if int(r.activeStreamsCount) < inputStreamsCount {
		return err
	}

	logger.Debugf(ctx, "sending out all the pending packets (%d), because the amount of streams is %d (/%d)", len(r.pendingPackets), int(r.activeStreamsCount), inputStreamsCount)
	for _, pkt := range r.pendingPackets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputPacketCh <- pkt:
		}
	}
	r.pendingPackets = r.pendingPackets[:0]
	r.started = true
	return err
}

func (r *Recoder[DF, EF]) process(
	ctx context.Context,
	input packet.Input,
	resultCh chan<- packet.Output,
) (_err error) {
	err := r.Encoder.SendInputPacket(ctx, input, resultCh, nil)
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrNotCopyEncoder{}):
	default:
		return fmt.Errorf("unable to encode the packet: %w", err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		cancelFn()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	framesCh := make(chan frame.Output, 2)
	wg.Add(1)
	var encoderError error
	observability.Go(ctx, func() {
		defer wg.Done()
		defer func() {
			cancelFn()
		}()
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

				err := r.Encoder.SendInputFrame(ctx, frame.Input(f), resultCh, nil)
				if err != nil {
					encoderError = err
				}
			}()
		}
	})

	err = r.Decoder.SendInputPacket(ctx, input, resultCh, framesCh)
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

func (r *Recoder[DF, EF]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.Encoder.WithOutputFormatContext(ctx, callback)
}

func (r *Recoder[DF, EF]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.Decoder.WithInputFormatContext(ctx, callback)
}

func (r *Recoder[DF, EF]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	var errs []error
	if err := r.Decoder.NotifyAboutPacketSource(ctx, source); err != nil {
		errs = append(errs, fmt.Errorf("decoder returned an error: %w", err))
	}
	if err := r.Encoder.NotifyAboutPacketSource(ctx, source); err != nil {
		errs = append(errs, fmt.Errorf("encoder returned an error: %w", err))
	}
	return errors.Join(errs...)
}

func (r *Recoder[DF, EF]) Reset(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()

	var errs []error
	if err := r.Encoder.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	if err := r.Decoder.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	return errors.Join(errs...)
}
