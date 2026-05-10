// transcoder.go implements the Transcoder kernel for decoding and then re-encoding media streams.
// In other words, it combines a Decoder and an Encoder into a single unit "Transcoder".

package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"sync"
	"time"

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
	// enableStreamCodecParametersUpdates re-publishes
	// outputStream.CodecParameters() from the current encoder whenever
	// the encoder's InitTS advances (i.e. on a Reinit). Required for
	// Bug 6.2: a resampler rebuild on input PCM format change forces an
	// audio-encoder reinit (encoder.go:reinitEncoderForResamplerRebuild),
	// regenerating the AAC ASC; without this re-publish branch firing,
	// the AVStream visible to downstream consumers retains the stale
	// extradata and the audio stream stays unreadable.
	//
	// The previous incarnation of the publish (commit 9f07679, reverted in
	// e99df5e) ran from the encoder hot path and re-acquired the same
	// non-reentrant codec lock that LockDo had just taken — deadlocking
	// the encoder, filling downstream queues, and forcing a route
	// consumer detach (audio dropped). The current publish runs from
	// inside drain's callback (see republishCodecParamsIfStale in
	// encoder.go), where the codec context is already locked and the
	// writer (cc.ToCodecParameters) does not re-acquire any encoder lock.
	enableStreamCodecParametersUpdates = true
	transcoderWaitForStreamsStart      = true

	// periodicReportInterval bounds how long an encoderError-suppressed
	// drop window can stay silent: if a dispatch cycle stays in the
	// "frames being dropped" state continuously, the goroutine emits a
	// periodic Warnf at this interval so operators see the cycle is
	// still in trouble. Without this, a sustained drop window emits
	// the first Warnf and then stays silent until the cycle ends —
	// which for a stuck cascade can be effectively forever. Sized for
	// the operator-attention window (60s feels recent in human-scale
	// log scanning) and large enough not to flood at full audio rate.
	periodicReportInterval = 60 * time.Second
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
	FilterCondition framecondition.Condition
	FilterKernel    Abstract
	Locker          xsync.Mutex

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
	encoderConfig *EncoderConfig,
) (_ret *Transcoder[DF, EF], _err error) {
	logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %s)", decoderFactory, encoderFactory, encoderConfig)
	defer func() {
		logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %s): %s, %v", decoderFactory, encoderFactory, encoderConfig, _ret, _err)
	}()
	r := &Transcoder[DF, EF]{
		ClosureSignaler: closuresignaler.New(),
		Decoder:         NewDecoder(ctx, decoderFactory),
		Encoder:         NewEncoder(ctx, encoderFactory, encoderConfig),

		activeStreamsMap: make(map[int]struct{}),
	}
	return r, nil
}

func (r *Transcoder[DF, EF]) SetFilterKernel(
	ctx context.Context,
	kernel Abstract,
) {
	xsync.DoA2(ctx, &r.Locker, func(ctx context.Context, kernel Abstract) {
		r.FilterKernel = kernel
	}, ctx, kernel)
}

func (r *Transcoder[DF, EF]) GetFilterKernel(
	ctx context.Context,
) Abstract {
	return xsync.DoA1R1(ctx, &r.Locker, func(ctx context.Context) Abstract {
		return r.FilterKernel
	}, ctx)
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
	// Read FilterKernel under the lock to avoid racing with SetFilterKernel.
	filterKernel := r.GetFilterKernel(ctx)
	if filterKernel != nil {
		if err := filterKernel.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the filter kernel: %w", err))
		}
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
			&r.Locker,
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

	// Wait for the goroutine to finish draining resultCh before
	// reading r.activeStreamsCount and r.pendingPacketsAndFrames.
	logger.Tracef(ctx, "waiting for the result channel to be drained")
	wg.Wait()

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
	// Vector A: encoderError state partitioned by astiav.MediaType so an
	// audio-lane encoder error does not drop video frames at the
	// dispatch gates (and vice-versa). The shared `var encoderError
	// error` ancestor was a single monotonic-latch across mediaTypes —
	// the architectural defect that motivated this task. Phase 2 design
	// §3 mechanism + §3.5 invariants:
	//
	//   INV-1 (first-wins per lane): once a mediaType lane records an
	//         error, subsequent setEncoderError calls for that lane are
	//         no-ops. Encoded by the `if _, ok := encoderErrors[mt]; !ok`
	//         guard inside setEncoderError below.
	//   INV-2 (bounded size): map size bounded by distinct mediaType
	//         enum values in the cascade (≤7 per astiav). No unbounded
	//         growth.
	//   INV-3 (errors.Is chain preserved): cycle-return aggregates all
	//         per-mediaType errors via errors.Join, which preserves
	//         errors.Is/errors.As traversal across each lane's wrapped
	//         error. Cycle-return contract change is documented in
	//         Phase 2 design §3.5.
	//
	// Thread safety: single mutex preserved (mirrors the prior
	// encoderErrorLocker discipline; map ops are O(1) under the lock;
	// G1 outer dispatch loop + G2 inner filterOutputCh goroutine both
	// serialize through encoderErrorsLocker).
	encoderErrors := map[astiav.MediaType]error{}
	var encoderErrorsLocker sync.Mutex
	setEncoderError := func(mt astiav.MediaType, err error) {
		encoderErrorsLocker.Lock()
		defer encoderErrorsLocker.Unlock()
		if _, ok := encoderErrors[mt]; !ok {
			encoderErrors[mt] = err
		}
	}
	getEncoderError := func(mt astiav.MediaType) error {
		encoderErrorsLocker.Lock()
		defer encoderErrorsLocker.Unlock()
		return encoderErrors[mt]
	}

	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		// Do NOT cancel the context here: when FilterKernel != nil, a
		// second goroutine (below) reads from filterOutputCh and calls
		// Encoder.SendInput. If we cancel the context before that
		// goroutine finishes, the encoder's drain→send sees
		// "context canceled" and the encoded packet is lost (the root
		// cause of h264_rkmpp producing 0 output packets). The outer
		// decoderToEncoder defer chain (wg.Wait then cancelFn) handles
		// cleanup in the correct order.

		var filterOutputCh chan packetorframe.OutputUnion
		if r.FilterKernel != nil {
			filterOutputCh = make(chan packetorframe.OutputUnion, 10)
			wg.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				// errAlreadyLogged + droppedCount surface the
				// "filterOutputCh frames silently dropped because an
				// encoderError was already latched" condition that
				// would otherwise be invisible. The rate-limiter has
				// two reset triggers (first-wins between them):
				//   (a) cycle ends — emits a summary with the total
				//       count via the deferred function below.
				//   (b) periodicReportInterval elapses since the last
				//       Warnf — emits a "still suppressing" Warnf with
				//       the cumulative count and clears errAlreadyLogged
				//       so the next drop re-emits an initial Warnf.
				// The deferred summary is registered before the loop so
				// a panic mid-loop still surfaces the count to operator
				// logs (panic-safety symmetry with the outer dispatch
				// cycle below).
				var errAlreadyLogged bool
				var droppedCount int
				defer func() {
					if droppedCount > 0 {
						logger.Warnf(ctx, "filterOutputCh cycle ended: %d frame(s) dropped due to latched encoderError", droppedCount)
					}
				}()
				ticker := time.NewTicker(periodicReportInterval)
				defer ticker.Stop()
				for {
					select {
					case out, ok := <-filterOutputCh:
						if !ok {
							return
						}
						// C1 LOAD-BEARING nil-deref guard. OutputUnion.Get()
						// returns a nil interface when both Frame and
						// Packet are nil (see packetorframe/packet_or_frame.go
						// L109-118); calling out.GetMediaType() in that
						// state panics on method-against-nil-interface.
						// The outer dispatch loop already guards against
						// nil-Frame at L458-466; we mirror that here for
						// the filterOutputCh case, adapted for the union's
						// Frame+Packet duality (filter kernels can emit
						// either; an empty union is degenerate but never
						// unreachable from the type system).
						if out.Frame == nil && out.Packet == nil {
							continue
						}
						mt := out.GetMediaType()
						if err := getEncoderError(mt); err != nil {
							droppedCount++
							if !errAlreadyLogged {
								logger.Warnf(ctx, "filterOutputCh frames being dropped (mediaType=%s): encoderError already latched: %v; suppressing further drop logs until cycle end or %s elapsed", mt, err, periodicReportInterval)
								errAlreadyLogged = true
							}
							continue
						}
						err := r.Encoder.SendInput(ctx, out.ToInput(), outputCh)
						if err != nil {
							setEncoderError(mt, err)
						}
					case <-ticker.C:
						if errAlreadyLogged && droppedCount > 0 {
							logger.Warnf(ctx, "filterOutputCh still suppressing further drops; current count %d", droppedCount)
							errAlreadyLogged = false
						}
					}
				}
			})
			defer close(filterOutputCh)
		}

		// resultErrAlreadyLogged + resultDroppedCount mirror the
		// filterOutputCh observability pattern above: surface the
		// "decoded frames silently skipped because an encoderError was
		// already latched" condition. Two reset triggers (first-wins):
		//   (a) cycle ends — deferred summary below emits the total.
		//   (b) periodicReportInterval elapses since the last Warnf —
		//       emits a "still suppressing" Warnf with the cumulative
		//       count and clears resultErrAlreadyLogged so the next
		//       skip re-emits an initial Warnf.
		var resultErrAlreadyLogged bool
		var resultDroppedCount int
		defer func() {
			if resultDroppedCount > 0 {
				logger.Warnf(ctx, "decoder→encoder dispatch cycle ended: %d frame(s) skipped due to latched encoderError", resultDroppedCount)
			}
		}()
		resultTicker := time.NewTicker(periodicReportInterval)
		defer resultTicker.Stop()
		for {
			var out packetorframe.OutputUnion
			var ok bool
			select {
			case out, ok = <-resultCh:
				if !ok {
					return
				}
			case <-resultTicker.C:
				if resultErrAlreadyLogged && resultDroppedCount > 0 {
					logger.Warnf(ctx, "decoder→encoder dispatch still suppressing further skips; current count %d", resultDroppedCount)
					resultErrAlreadyLogged = false
				}
				continue
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
			mt := f.GetMediaType()
			logger.Tracef(ctx, "got a decoded %s frame from the decoder", mt)
			func() {
				defer frame.Pool.Put(f.Frame)
				if err := getEncoderError(mt); err != nil {
					resultDroppedCount++
					if !resultErrAlreadyLogged {
						logger.Warnf(ctx, "decoder→encoder dispatch skipping frames (mediaType=%s): encoderError already latched: %v; suppressing further skip logs until cycle end or %s elapsed", mt, err, periodicReportInterval)
						resultErrAlreadyLogged = true
					}
					return
				}

				inputFrame := frame.Input(f)
				if r.FilterCondition != nil && !r.FilterCondition.Match(ctx, inputFrame) {
					logger.Tracef(ctx, "frame filtered out by condition")
					return
				}

				if r.FilterKernel == nil {
					err := r.Encoder.SendInput(ctx, packetorframe.InputUnion{Frame: &inputFrame}, outputCh)
					if err != nil {
						logger.Tracef(ctx, "encoder returned an error: %v", err)
						setEncoderError(mt, err)
					}
					return
				}

				err := r.FilterKernel.SendInput(ctx, packetorframe.InputUnion{Frame: &inputFrame}, filterOutputCh)
				if err != nil {
					logger.Tracef(ctx, "filter kernel returned an error: %v", err)
					setEncoderError(mt, err)
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
	// Vector A cycle-return aggregation: if any per-mediaType lane
	// recorded an error, wrap them all via errors.Join. Sort by the
	// astiav.MediaType integer value before joining for deterministic
	// ordering — Phase 2 design §10 critique #1 (map iteration is
	// nondeterministic; tests + log readers expect stable order). The
	// errors.Join pattern is project-canonical: 42 existing call sites
	// in kernel/*.go top-level (post-Vector-A this becomes 43; verified
	// per-file via `grep -cE "errors\.Join" kernel/*.go` at canonical
	// f49380c1 this session).
	encoderErrorsLocker.Lock()
	mediaTypes := make([]astiav.MediaType, 0, len(encoderErrors))
	for mt := range encoderErrors {
		mediaTypes = append(mediaTypes, mt)
	}
	encoderErrorsLocker.Unlock()
	slices.Sort(mediaTypes)
	if len(mediaTypes) > 0 {
		encErrs := make([]error, 0, len(mediaTypes))
		for _, mt := range mediaTypes {
			encErrs = append(encErrs, fmt.Errorf("mediaType=%s: %w", mt, encoderErrors[mt]))
		}
		return fmt.Errorf("got error(s) from the encoder: %w", errors.Join(encErrs...))
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
	r.Locker.Do(ctx, func() {
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
	if r.FilterCondition != nil && !r.FilterCondition.Match(ctx, input) {
		logger.Tracef(ctx, "frame filtered out by condition")
		return nil
	}

	if r.FilterKernel == nil {
		return r.Encoder.SendInput(ctx, packetorframe.InputUnion{Frame: &input}, outputCh)
	}

	filterOutputCh := make(chan packetorframe.OutputUnion, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	var encoderErr error
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		for out := range filterOutputCh {
			err := r.Encoder.SendInput(ctx, out.ToInput(), outputCh)
			if err != nil && encoderErr == nil {
				encoderErr = err
			}
		}
	})
	err := r.FilterKernel.SendInput(ctx, packetorframe.InputUnion{Frame: &input}, filterOutputCh)
	close(filterOutputCh)
	wg.Wait()
	return errors.Join(err, encoderErr)
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
		errs = append(errs, fmt.Errorf("unable to reset the decoder: %w", err))
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
		errs = append(errs, fmt.Errorf("unable to reset the decoder: %w", err))
	}
	return errors.Join(errs...)
}

func (r *Transcoder[DF, EF]) SetForceNextKeyFrame(
	ctx context.Context,
	v bool,
) error {
	return r.Encoder.SetForceNextKeyFrame(ctx, v)
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
