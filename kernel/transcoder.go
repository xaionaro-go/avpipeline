// transcoder.go implements the Transcoder kernel for decoding and then re-encoding media streams.

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
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	enableStreamCodecParametersUpdates = false
	transcoderWaitForStreamsStart      = true
)

// Transcoder is a kernel that decodes and then encodes packets/frames.
// It effectively combines a Decoder and an Encoder into a single unit.
//
// See also https://github.com/namndev/FFmpegTutorial/blob/master/learn-ffmpeg-libav-the-hard-way.md
// Note: Transcoder is a somewhat hacky thing, try to not use it. Pipelining
// should be handled by pipeline, not by a Kernel. Use separately Decoder and Encoder, instead.
type Transcoder[DF codec.DecoderFactory, EF codec.EncoderFactory] struct {
	*Decoder[DF]
	*Encoder[EF]
	*closuresignaler.ClosureSignaler
	Filter framecondition.Condition

	locker                  xsync.Mutex
	flushLocker             xsync.Mutex
	started                 bool
	activeStreamsMap        map[int]struct{}
	activeStreamsCount      uint
	pendingPacketsAndFrames []packetorframe.OutputUnion
}

var (
	_ Abstract      = (*Transcoder[codec.DecoderFactory, codec.EncoderFactory])(nil)
	_ packet.Source = (*Transcoder[codec.DecoderFactory, codec.EncoderFactory])(nil)
	_ packet.Sink   = (*Transcoder[codec.DecoderFactory, codec.EncoderFactory])(nil)
)

func NewTranscoder[DF codec.DecoderFactory, EF codec.EncoderFactory](
	ctx context.Context,
	decoderFactory DF,
	encoderFactory EF,
	streamConfigurer StreamConfigurer,
) (_ret *Transcoder[DF, EF], _err error) {
	logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v)", decoderFactory, encoderFactory, streamConfigurer)
	defer func() {
		logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v): %s, %v", decoderFactory, encoderFactory, streamConfigurer, _ret, _err)
	}()
	r := &Transcoder[DF, EF]{
		ClosureSignaler: closuresignaler.New(),
		Decoder:         NewDecoder(ctx, decoderFactory),
		Encoder:         NewEncoder(ctx, encoderFactory, streamConfigurer),

		activeStreamsMap: make(map[int]struct{}),
	}
	return r, nil
}

func (r *Transcoder[DF, EF]) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	r.ClosureSignaler.Close(ctx)
	var errs []error
	if err := r.Decoder.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the decoder: %w", err))
	}
	if err := r.Encoder.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the encoder: %w", err))
	}
	return errors.Join(errs...)
}

func (r *Transcoder[DF, EF]) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (r *Transcoder[DF, EF]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	pkt, frame := input.Unwrap()
	switch {
	case pkt != nil:
		logger.Tracef(ctx, "SendInput(packet)")
		defer func() { logger.Tracef(ctx, "/SendInput(packet): %v", _err) }()
		return xsync.DoA3R1(
			ctx,
			&r.locker,
			r.sendPacketNoLock,
			ctx,
			*pkt,
			outputCh,
		)
	case frame != nil:
		return r.sendFrame(ctx, *frame, outputCh)
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
}

func (r *Transcoder[DF, EF]) sendPacketNoLock(
	ctx context.Context,
	input packet.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	mediaType := input.GetMediaType()
	logger.Tracef(ctx, "sendPacket %s (started: %v)", mediaType, r.started)
	defer func() { logger.Tracef(ctx, "/sendPacket: %v: %v (started: %v)", mediaType, _err, r.started) }()

	if r.IsClosed() {
		return io.ErrClosedPipe
	}

	if r.started || !transcoderWaitForStreamsStart {
		return r.process(ctx, input, outputCh)
	}

	resultCh := make(chan packetorframe.OutputUnion, 1)
	var wg sync.WaitGroup
	defer func() {
		logger.Tracef(ctx, "waiting for the result channel to be closed")
		wg.Wait()
	}()
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "result channel closed")
		for out := range resultCh {
			r.pendingPacketsAndFrames = append(r.pendingPacketsAndFrames, out)
			if len(r.pendingPacketsAndFrames) > pendingPacketsAndFramesLimit {
				logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
				r.pendingPacketsAndFrames = r.pendingPacketsAndFrames[1:]
			}
			streamIdx := out.GetStreamIndex()

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
			close(resultCh)
			panic(r)
		}
	}()
	err := r.process(ctx, input, resultCh)
	logger.Tracef(ctx, "closing the result channels")
	close(resultCh)

	inputStreamsCount := sourceNbStreams(ctx, input.GetSource())
	logger.Tracef(ctx, "input streams count: %d (source: %s), active streams count: %d", inputStreamsCount, input.GetSource(), r.activeStreamsCount)
	if inputStreamsCount > int(r.activeStreamsCount) {
		return err
	}

	logger.Debugf(ctx, "sending out all the pending packets (%d), because the amount of streams is %d (/%d)", len(r.pendingPacketsAndFrames), int(r.activeStreamsCount), inputStreamsCount)
	for _, pktOrFrame := range r.pendingPacketsAndFrames {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputCh <- pktOrFrame:
		}
	}
	r.pendingPacketsAndFrames = r.pendingPacketsAndFrames[:0]
	r.started = true
	return err
}

func (r *Transcoder[DF, EF]) process(
	ctx context.Context,
	input packet.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "process")
	defer func() { logger.Tracef(ctx, "/process: %v", _err) }()

	// try copying first (e.g. in case '-c:v copy' is used):

	err := r.Encoder.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, outputCh)
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
		outputCh chan<- packetorframe.OutputUnion,
	) error {
		return r.Decoder.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, outputCh)
	}, outputCh)
}

func (r *Transcoder[DF, EF]) decoderToEncoder(
	ctx context.Context,
	decodeFn func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error,
	outputCh chan<- packetorframe.OutputUnion,
) (_ret error) {
	logger.Tracef(ctx, "decoderToEncoder")
	defer func() { logger.Tracef(ctx, "/decoderToEncoder: %v", _ret) }()

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		cancelFn()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	resultCh := make(chan packetorframe.OutputUnion, 2)
	wg.Add(1)
	var encoderError error
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer func() {
			cancelFn()
		}()
		for {
			out, ok := <-resultCh
			if !ok {
				return
			}
			if out.Frame == nil {
				logger.Tracef(ctx, "got a non-frame output from the decoder; passing it through")
				select {
				case <-ctx.Done():
					return
				case outputCh <- out:
				}
				continue
			}
			f := *out.Frame
			logger.Tracef(ctx, "got a decoded %s frame from the decoder", f.GetMediaType())
			func() {
				defer frame.Pool.Put(f.Frame)
				if encoderError != nil {
					logger.Tracef(ctx, "skipping encoding because there is already an encoder error: %v", encoderError)
					return
				}

				inputFrame := frame.Input(f)
				if r.Filter != nil && !r.Filter.Match(ctx, inputFrame) {
					logger.Tracef(ctx, "frame filtered out")
					return
				}
				err := r.Encoder.SendInput(ctx, packetorframe.InputUnion{Frame: &inputFrame}, outputCh)
				if err != nil {
					logger.Tracef(ctx, "encoder returned an error: %v", err)
					encoderError = err
				}
			}()
		}
	})

	var err error
	func() {
		defer close(resultCh)
		err = decodeFn(ctx, resultCh)
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

func (r *Transcoder[DF, EF]) sendFrame(
	ctx context.Context,
	input frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sendFrame")
	defer func() { logger.Tracef(ctx, "/sendFrame: %v", _err) }()
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
	return r.Encoder.SendInput(ctx, packetorframe.InputUnion{Frame: &input}, outputCh)
}

func (r *Transcoder[DF, EF]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(r)
}

func (r *Transcoder[DF, EF]) String() string {
	return fmt.Sprintf("Transcoder(%s->%s)", r.DecoderFactory, r.EncoderFactory)
}

func (r *Transcoder[DF, EF]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.Encoder.WithOutputFormatContext(ctx, callback)
}

func (r *Transcoder[DF, EF]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.Decoder.WithInputFormatContext(ctx, callback)
}

func (r *Transcoder[DF, EF]) NotifyAboutPacketSource(
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

func (r *Transcoder[DF, EF]) ResetSoft(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetSoft")
	defer func() { logger.Debugf(ctx, "/ResetSoft: %v", _err) }()

	var errs []error
	if err := r.Encoder.ResetSoft(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	if err := r.Decoder.ResetSoft(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	return errors.Join(errs...)
}

func (r *Transcoder[DF, EF]) ResetHard(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetHard")
	defer func() { logger.Debugf(ctx, "/ResetHard: %v", _err) }()

	var errs []error
	if err := r.Encoder.ResetHard(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	if err := r.Decoder.ResetHard(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder: %w", err))
	}
	return errors.Join(errs...)
}

func (r *Transcoder[DF, EF]) SetForceNextKeyFrame(
	ctx context.Context,
	v bool,
) {
	r.Encoder.SetForceNextKeyFrame(ctx, v)
}

func (r *Transcoder[DF, EF]) IsDirty(
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

var _ Flusher = (*Transcoder[codec.DecoderFactory, codec.EncoderFactory])(nil)

func (r *Transcoder[DF, EF]) Flush(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Flush")
	defer func() { logger.Debugf(ctx, "/Flush: %v", _err) }()
	var errs []error

	r.flushLocker.Do(ctx, func() {
		if err := r.decoderToEncoder(ctx, func(
			ctx context.Context,
			outputCh chan<- packetorframe.OutputUnion,
		) (_err error) {
			defer func() {
				r := recover()
				if r != nil {
					_err = fmt.Errorf("panic: %v:\n%s", r, debug.Stack())
				}
			}()
			return r.Decoder.Flush(ctx, outputCh)
		}, outputCh); err != nil {
			errs = append(errs, fmt.Errorf("unable to flush the decoder: %w", err))
		}

		if err := r.Encoder.Flush(ctx, outputCh); err != nil {
			errs = append(errs, fmt.Errorf("unable to flush the encoder: %w", err))
		}
	})

	return errors.Join(errs...)
}
