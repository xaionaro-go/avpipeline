package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
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
	*closuresignaler.ClosureSignaler
	Filter framecondition.Condition

	locker                  xsync.Mutex
	started                 bool
	activeStreamsMap        map[int]struct{}
	activeStreamsCount      uint
	pendingPacketsAndFrames []packetorframe.Abstract
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
		ClosureSignaler: closuresignaler.New(),
		Decoder:         NewDecoder(ctx, decoderFactory),
		Encoder:         NewEncoder(ctx, encoderFactory, streamConfigurer),

		activeStreamsMap: make(map[int]struct{}),
	}
	return r, nil
}

func (r *Recoder[DF, EF]) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	r.ClosureSignaler.Close(ctx)
	r.Decoder.ClosureSignaler.Close(ctx)
	r.Encoder.ClosureSignaler.Close(ctx)
	return nil
}

func (r *Recoder[DF, EF]) Generate(
	ctx context.Context,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
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
	return xsync.DoA4R1(
		ctx,
		&r.locker,
		r.sendInputPacket,
		ctx,
		input,
		outputPacketsCh, outputFramesCh,
	)
}

func (r *Recoder[DF, EF]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "sendInputPacket (started: %v)", r.started)
	defer func() { logger.Tracef(ctx, "/sendInputPacket: %v (started: %v)", _err, r.started) }()

	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	if r.started {
		return r.process(ctx, input, outputPacketCh, outputFramesCh)
	}

	resultPacketsCh := make(chan packet.Output, 1)
	resultFramesCh := make(chan frame.Output, 1)
	var wg sync.WaitGroup
	defer func() {
		logger.Tracef(ctx, "waiting for the result channel to be closed")
		wg.Wait()
	}()
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "result channel closed")
		for {
			var streamIdx int
			select {
			case pkt, ok := <-resultPacketsCh:
				if !ok {
					return
				}
				r.pendingPacketsAndFrames = append(r.pendingPacketsAndFrames, &pkt)
				if len(r.pendingPacketsAndFrames) > pendingPacketsAndFramesLimit {
					logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
					r.pendingPacketsAndFrames = r.pendingPacketsAndFrames[1:]
				}
				streamIdx = pkt.Stream.Index()
			case frame, ok := <-resultFramesCh:
				if !ok {
					return
				}
				r.pendingPacketsAndFrames = append(r.pendingPacketsAndFrames, &frame)
				if len(r.pendingPacketsAndFrames) > pendingPacketsAndFramesLimit {
					logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
					r.pendingPacketsAndFrames = r.pendingPacketsAndFrames[1:]
				}
				streamIdx = frame.StreamIndex
			}

			if _, ok := r.activeStreamsMap[streamIdx]; ok {
				continue
			}
			r.activeStreamsCount++
			r.activeStreamsMap[streamIdx] = struct{}{}
		}
	})

	defer func() {
		r := recover()
		if r != nil {
			close(resultPacketsCh)
			panic(r)
		}
	}()
	err := r.process(ctx, input, resultPacketsCh, resultFramesCh)
	logger.Tracef(ctx, "closing the result channels")
	close(resultPacketsCh)
	close(resultFramesCh)

	inputStreamsCount := sourceNbStreams(ctx, input.Source)
	logger.Tracef(ctx, "input streams count: %d, active streams count: %d", inputStreamsCount, r.activeStreamsCount)
	if int(r.activeStreamsCount) < inputStreamsCount {
		return err
	}

	logger.Debugf(ctx, "sending out all the pending packets (%d), because the amount of streams is %d (/%d)", len(r.pendingPacketsAndFrames), int(r.activeStreamsCount), inputStreamsCount)
	for _, pktOrFrame := range r.pendingPacketsAndFrames {
		switch pktOrFrame := pktOrFrame.(type) {
		case *packet.Output:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case outputPacketCh <- *pktOrFrame:
			}
		case *frame.Output:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case outputFramesCh <- *pktOrFrame:
			}
		default:
			return fmt.Errorf("unexpected type of pending packet/frame: %T", pktOrFrame)
		}
	}
	r.pendingPacketsAndFrames = r.pendingPacketsAndFrames[:0]
	r.started = true
	return err
}

func (r *Recoder[DF, EF]) process(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "process")
	defer func() { logger.Tracef(ctx, "/process: %v", _err) }()

	// try copying first (e.g. in case '-c:v copy' is used):

	err := r.Encoder.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrNotCopyEncoder{}):
	default:
		return fmt.Errorf("unable to encode the packet: %w", err)
	}

	// OK, this is a not a case for a copying, we have to actually decode and then encode:

	return r.decoderToEncoder(ctx, func(
		ctx context.Context,
		outputFramesCh chan<- frame.Output,
	) error {
		return r.Decoder.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	}, outputPacketsCh, outputFramesCh)
}

func (r *Recoder[DF, EF]) decoderToEncoder(
	ctx context.Context,
	decodeFn func(ctx context.Context, outputFramesCh chan<- frame.Output) error,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_ret error) {
	logger.Tracef(ctx, "recode")
	defer func() { logger.Tracef(ctx, "/recode: %v", _ret) }()

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		cancelFn()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	framesCh := make(chan frame.Output, 2)
	wg.Add(1)
	var encoderError error
	observability.Go(ctx, func(ctx context.Context) {
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

				inputFrame := frame.Input(f)
				if r.Filter != nil && !r.Filter.Match(ctx, inputFrame) {
					logger.Tracef(ctx, "frame filtered out")
					return
				}
				err := r.Encoder.SendInputFrame(ctx, inputFrame, outputPacketsCh, outputFramesCh)
				if err != nil {
					encoderError = err
				}
			}()
		}
	})

	var err error
	func() {
		defer close(framesCh)
		err = decodeFn(ctx, framesCh)
	}()
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
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	r.locker.Do(ctx, func() {
		if r.started {
			return
		}
		streamIdx := input.StreamIndex
		if _, ok := r.activeStreamsMap[streamIdx]; ok {
			return
		}
		r.activeStreamsCount++
		r.activeStreamsMap[streamIdx] = struct{}{}
	})
	if r.Filter != nil && !r.Filter.Match(ctx, input) {
		logger.Tracef(ctx, "frame filtered out")
		return nil
	}
	return r.Encoder.SendInputFrame(ctx, input, outputPacketsCh, outputFramesCh)
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

func (r *Recoder[DF, EF]) IsDirty(
	ctx context.Context,
) (_ret bool) {
	logger.Tracef(ctx, "IsDirty")
	defer func() { logger.Tracef(ctx, "/IsDirty: %v", _ret) }()
	var wg sync.WaitGroup
	var r0, r1 bool
	wg.Add(1)
	observability.Go(context.Background(), func(ctx context.Context) {
		defer wg.Done()
		r0 = r.Decoder.IsDirty(ctx)
	})
	wg.Add(1)
	observability.Go(context.Background(), func(ctx context.Context) {
		defer wg.Done()
		r1 = r.Encoder.IsDirty(ctx)
	})
	wg.Wait()
	return r0 || r1
}

var _ Flusher = (*Recoder[codec.DecoderFactory, codec.EncoderFactory])(nil)

func (r *Recoder[DF, EF]) Flush(
	ctx context.Context,
	outputPacketCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Debugf(ctx, "Flush")
	defer func() { logger.Debugf(ctx, "/Flush: %v", _err) }()
	var errs []error

	if err := r.decoderToEncoder(ctx, func(
		ctx context.Context,
		outputFramesCh chan<- frame.Output,
	) (_err error) {
		defer func() {
			r := recover()
			if r != nil {
				_err = fmt.Errorf("panic: %v:\n%s", r, debug.Stack())
			}
		}()
		return r.Decoder.Flush(ctx, outputPacketCh, outputFramesCh)
	}, outputPacketCh, outputFramesCh); err != nil {
		errs = append(errs, fmt.Errorf("unable to flush the decoder: %w", err))
	}

	if err := r.Encoder.Flush(ctx, outputPacketCh, outputFramesCh); err != nil {
		errs = append(errs, fmt.Errorf("unable to flush the encoder: %w", err))
	}
	return errors.Join(errs...)
}
