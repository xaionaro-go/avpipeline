// encoder.go implements the Encoder kernel for encoding media frames into packets.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/resampler"
	"github.com/xaionaro-go/avpipeline/scaler"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	encoderWriteHeaderOnFinishedGettingStreams = false
	encoderWriteHeaderOnNotifyPacketSources    = false
	encoderForceCopyTime                       = true
	encoderCopyTimeAfterScaling                = true
	encoderDTSHigherPTSCorrect                 = false
	encoderDebug                               = true
	encoderExtraDefensive                      = true
	encoderRescaleEnableCropping               = false
	encoderRescaleSameResolution               = false
	encoderRescaleEnableTightPacking           = false

	canBypassScaler = !encoderRescaleSameResolution &&
		!encoderRescaleEnableCropping &&
		!encoderRescaleEnableTightPacking

	// sendFrameMaxRetries bounds the inner EAGAIN-drain retry loop.
	// 1 legitimate prime (encoder accepts after first drain) +
	// 1 grace cycle. Beyond that we treat the encoder as genuinely
	// stalled (e.g. av1_mediacodec output queue not advancing) and
	// bail to let the caller escalate to Reinit.
	sendFrameMaxRetries = 2

	// encoderStallReinitThreshold is the consecutive-stalled-frame
	// count that triggers a full encoder Reinit. 3 stalled frames in
	// a row ≈ ~100 ms of stall at 30 fps — clearly not a transient
	// EAGAIN, and not enough delay to harm interactive latency once
	// recovery succeeds.
	encoderStallReinitThreshold = 3

	// encoderSilentConsumeThreshold is the per-streamEncoder count of
	// SendFrame=nil successes since the last drain-emitted packet that
	// triggers a full Reinit. 60 input frames ≈ ~2 s of silent buffering
	// at 30 fps — long enough to rule out the legitimate codec startup
	// delay (B-frame look-ahead, hardware encoder warm-up), short enough
	// to recover before downstream FIFOs back-pressure the input chain.
	//
	// This complements encoderStallReinitThreshold (EAGAIN-loop class):
	// silent-consume is a different failure mode where SendFrame keeps
	// returning nil but Drain emits nothing. Both routes converge on the
	// same Reinit path.
	encoderSilentConsumeThreshold = 60
)

// codecParamsRepublishWarnBackoff caps the rate at which
// republishCodecParamsIfStale emits its writer-error Warnf when the
// writer (cc.ToCodecParameters) keeps failing on every drained packet.
// At AAC frame rate (~50 fps for 1024-sample frames at 48 kHz) the
// pre-backoff helper would emit 50 warns/sec — observable as a log
// flood that masks any other useful output. One warn per second is
// sufficient to surface the issue without spamming.
const codecParamsRepublishWarnBackoff = time.Second

type Encoder[EF codec.EncoderFactory] struct {
	*closuresignaler.ClosureSignaler
	EncoderConfig

	EncoderFactory EF
	Locker         xsync.Mutex
	PTSDurDiff     *time.Duration

	encoders                  map[int]*streamEncoder
	outputStreams             map[int]*astiav.Stream
	outputFormatContextLocker xsync.RWMutex
	outputFormatContext       *astiav.FormatContext
	headerIsWritten           bool
	isDirtyCache              atomic.Bool
	forceNextKeyFrame         atomic.Bool
}

var (
	_ Abstract      = (*Encoder[codec.EncoderFactory])(nil)
	_ packet.Source = (*Encoder[codec.EncoderFactory])(nil)
)

type streamEncoder struct {
	Encoder         codec.Encoder
	EncoderConfig   *EncoderConfig
	Resampler       *resampler.Resampler
	ResampledFrames []*astiav.Frame
	Scaler          scaler.Scaler
	ScaledFrame     *astiav.Frame
	LastInitTS      time.Time

	// lastCodecParamsRepublishWarnAt rate-limits the writer-error Warnf
	// in republishCodecParamsIfStale. When the writer (cc.ToCodecParameters
	// in production) keeps failing, the helper would otherwise re-fire on
	// every drained packet — at AAC frame rate (~50fps for 1024-sample
	// frames at 48kHz) that's a 50-msg-per-second log flood. The backoff
	// keeps the warn at most once per codecParamsRepublishWarnBackoff.
	// Successful republish explicitly zeros the stamp (see the
	// post-success branch in republishCodecParamsIfStale) so a new
	// failure window after the next reinit is not swallowed by a
	// leftover backoff.
	lastCodecParamsRepublishWarnAt time.Time

	// FrameInfoFIFO tracks per-frame timestamps for codecs with encoder
	// delay that don't support opaque data round-trip (e.g. AAC). Each
	// SendFrame pushes a FrameInfo, and each ReceivePacket pops one,
	// maintaining correct timestamp correspondence.
	FrameInfoFIFO []FrameInfo

	// audioResampleNextOutPTS / HaveOutAnchor restamp resampled audio
	// output frames with timestamps in the output sample-rate grid. The
	// resampler returns frames with no PTS/Duration, so without this the
	// encoder would inherit the *input* frame's PTS+Duration — causing a
	// (1 - inRate/outRate) per-frame DTS gap on cross-rate resampling
	// (e.g. 44100->48000 ≈ 8% audio-continuity deficit).
	audioResampleNextOutPTS    int64
	audioResampleHaveOutAnchor bool

	// consecutiveStalls tracks how many input frames in a row failed to
	// be accepted by SendFrame after the bounded drain-retry budget was
	// exhausted. Reset on any successful SendFrame. When the count
	// reaches encoderStallReinitThreshold, the wrapping caller
	// (sendFrame) escalates to a full codec Reinit (av1_mediacodec
	// genuine stall watchdog).
	consecutiveStalls atomic.Uint64

	// drainPacketCount accumulates the number of packets emitted by the
	// drain callback over the lifetime of this streamEncoder. Used by
	// the bounded retry loop to detect "drain produced no output", in
	// which case retrying SendFrame would just spin. Atomic because the
	// drain callback may be invoked from a different goroutine context
	// in some encoder backends.
	drainPacketCount atomic.Uint64

	// framesInSinceLastPacket counts input frames accepted by SendFrame
	// (i.e. SendFrame returned nil) since the last packet was emitted by
	// the drain callback for this streamEncoder. Reset to 0 every time
	// the drain callback fires; incremented by sendFrameWithDrainRetry
	// on every successful SendFrame.
	//
	// This catches the "silent-consume" stall class:
	// MediaCodec accepts frames (no EAGAIN, no error) but never produces
	// any output. The EAGAIN-loop watchdog (consecutiveStalls) does
	// not fire here because SendFrame keeps succeeding — input
	// piles up at the codec while output is frozen.
	//
	// When this counter crosses encoderSilentConsumeThreshold the caller
	// (sendFrame) escalates to a full codec Reinit using the same path
	// as the EAGAIN-stall watchdog (codec.EncoderReiniter + FIFO clear).
	framesInSinceLastPacket atomic.Uint64
}

func (e *streamEncoder) Close(ctx context.Context) error {
	// Close the codec encoder first (this drains any buffered frames)
	err := e.Encoder.Close(ctx)
	// Prevent GC from collecting ScaledFrame before the encoder is done draining
	runtime.KeepAlive(e.ScaledFrame)
	return err
}

// closeLocked closes the encoder using the locked variant supplied by
// codec.Encoder.LockDo. Mirrors Close but routes the close through the
// already-held locker (the locked variant's Close does not re-acquire
// codec.EncoderFull.locker), avoiding the self-deadlock that
// streamEncoder.Close would trigger when called from inside LockDo:
// Close -> EncoderFull.Close -> withLocked -> re-acquire same locker.
func (e *streamEncoder) closeLocked(ctx context.Context, locked codec.Encoder) error {
	err := locked.Close(ctx)
	// Prevent GC from collecting ScaledFrame before the encoder is done draining
	runtime.KeepAlive(e.ScaledFrame)
	return err
}

type EncoderConfig struct {
	StreamConfigurer StreamConfigurer
}

func (cfg *EncoderConfig) String() string {
	if cfg == nil {
		return "<nil>"
	}
	type alias EncoderConfig
	return spew.Sdump(alias(*cfg))
}

func DefaultEncoderConfig() EncoderConfig {
	return EncoderConfig{}
}

func NewEncoder[EF codec.EncoderFactory](
	ctx context.Context,
	encoderFactory EF,
	encoderConfig *EncoderConfig,
) (_ret *Encoder[EF]) {
	logger.Tracef(ctx, "NewEncoder")
	defer func() { logger.Tracef(ctx, "/NewEncoder: %s", _ret) }()
	if encoderConfig == nil {
		encoderConfig = ptr(DefaultEncoderConfig())
	}
	e := &Encoder[EF]{
		ClosureSignaler:     closuresignaler.New(),
		EncoderFactory:      encoderFactory,
		EncoderConfig:       *encoderConfig,
		encoders:            map[int]*streamEncoder{},
		outputFormatContext: astiav.AllocFormatContext(),
		outputStreams:       make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, e.outputFormatContext)
	return e
}

func (e *Encoder[EF]) Close(ctx context.Context) (_err error) {
	return xsync.DoA1R1(ctx, &e.Locker, e.closeLocked, ctx)
}

func (e *Encoder[EF]) closeLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "closeLocked ts_ms=%d", logger.NowMS())
	defer func() { logger.Debugf(ctx, "/closeLocked ts_ms=%d: %v", logger.NowMS(), _err) }()
	e.ClosureSignaler.Close(ctx)
	var errs []error
	if err := e.EncoderFactory.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder factory: %w", err))
	}
	for key, encoder := range e.encoders {
		if err := encoder.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the encoder for stream #%d: %w", key, err))
		}
		delete(e.encoders, key)
	}
	e.outputFormatContextLocker.Do(ctx, func() {
		if err := e.outputFormatContext.Flush(); err != nil {
			errs = append(errs, fmt.Errorf("unable to flush the output format context: %w", err))
		}
	})
	return errors.Join(errs...)
}

func (e *Encoder[EF]) initOutputStreamCopy(
	ctx context.Context,
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStreamCopy: streamIndex: %d", streamIndex)
	defer func() {
		if _err == nil {
			assert(ctx, e.outputStreams[streamIndex] != nil)
		}
		logger.Tracef(ctx, "/lazyInitOutputStreamCopy: streamIndex: %d: %v", streamIndex, _err)
	}()
	logger.Debugf(ctx, "new output (copy) stream (stream index: %d)", streamIndex)

	codec := astiav.FindDecoder(params.CodecID())
	var outputStream *astiav.Stream
	e.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = e.outputFormatContext.NewStream(codec)
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an (copy) output stream")
	}
	err := params.Copy(outputStream.CodecParameters())
	if err != nil {
		return fmt.Errorf("unable to copy codec parameters: %w", err)
	}

	err = e.configureOutputStream(ctx, outputStream, streamIndex, timeBase)
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) initOutputStream(
	ctx context.Context,
	streamIndex int,
	encoder codec.Encoder,
) (_err error) {
	logger.Tracef(ctx, "lazyInitOutputStream: streamIndex: %d", streamIndex)
	defer func() {
		if _err == nil && e.outputStreams[streamIndex] == nil {
			_err = fmt.Errorf("internal error: output stream for stream index %d is somehow still nil after an explicit request for initialization", streamIndex)
		}
		logger.Tracef(ctx, "/lazyInitOutputStream: streamIndex: %d: %v", streamIndex, _err)
	}()

	logger.Debugf(ctx, "new output stream (stream index: %d)", streamIndex)

	var outputStream *astiav.Stream
	e.outputFormatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		outputStream = e.outputFormatContext.NewStream(encoder.Codec(ctx))
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an output stream")
	}
	if err := encoder.ToCodecParameters(ctx, outputStream.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy codec parameters from the encoder to the output stream: %w", err)
	}

	err := e.configureOutputStream(ctx, outputStream, streamIndex, encoder.CodecContext(ctx).TimeBase())
	if err != nil {
		return fmt.Errorf("unable to configure the stream: %w", err)
	}

	return nil
}

func (e *Encoder[EF]) configureOutputStream(
	ctx context.Context,
	outputStream *astiav.Stream,
	streamIndex int,
	timeBase astiav.Rational,
) (_err error) {
	defer func() {
		if _err == nil && e.outputStreams[streamIndex] == nil {
			_err = fmt.Errorf("internal error: output stream for stream index %d is somehow still nil after an explicit request for initialization", streamIndex)
		}
	}()
	outputStream.SetIndex(streamIndex)
	outputStream.SetTimeBase(timeBase)
	if e.StreamConfigurer != nil {
		err := e.StreamConfigurer.StreamConfigure(ctx, outputStream, streamIndex)
		if err != nil {
			return fmt.Errorf("unable to configure the output stream: %w", err)
		}
	}
	logger.Debugf(
		ctx,
		"resulting output stream for input stream %d: %d: %s: %s: %s: %s: %s; extraData: %s",
		streamIndex,
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
		extradata.Raw(outputStream.CodecParameters().ExtraData()),
	)
	e.outputStreams[streamIndex] = outputStream

	return nil
}

func (e *Encoder[EF]) initEncoderAndOutputFor(
	ctx context.Context,
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	frameSource frame.Source,
) (_err error) {
	if params == nil {
		return fmt.Errorf("codec parameters must be set")
	}
	res := codec.Resolution{
		Width:  uint32(params.Width()),
		Height: uint32(params.Height()),
	}
	codecID := params.CodecID()
	pixFmt := params.PixelFormat()
	logger.Debugf(ctx, "initEncoderAndOutputFor(%d): %s, %v/%s, %T", streamIndex, codecID, res, pixFmt, frameSource)
	defer func() {
		logger.Debugf(ctx, "/initEncoderAndOutputFor(%d): %s, %v/%s, %T: %v", streamIndex, codecID, res, pixFmt, frameSource, _err)
	}()
	if _, ok := e.encoders[streamIndex]; ok {
		logger.Errorf(ctx, "stream #%d already exists, not initializing", streamIndex)
		return nil
	}

	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		logger.Tracef(ctx, "FPS: %v", params.FrameRate())
	case astiav.MediaTypeAudio:
		logger.Tracef(ctx, "SampleRate: %d", params.SampleRate())
	}
	err := e.initEncoderFor(ctx, streamIndex, params, timeBase, frameSource)
	if err != nil {
		return fmt.Errorf("(encoder) unable to initialize an output stream for input stream #%d: %w", streamIndex, err)
	}

	encoder := e.encoders[streamIndex]
	if encoder == nil {
		return fmt.Errorf("internal error: encoder for stream index %d is nil after an explicit request for initialization", streamIndex)
	}
	if _, ok := e.outputStreams[streamIndex]; ok {
		logger.Warnf(ctx, "output stream for stream index %d already exists; reusing (was the Encoder kernel reset?)", streamIndex)
		return nil
	}
	switch {
	case codec.IsEncoderCopy(encoder.Encoder):
		err = e.initOutputStreamCopy(ctx, streamIndex, params, timeBase)
	case codec.IsEncoderRaw(encoder.Encoder):
		return nil
	default:
		err = e.initOutputStream(ctx, streamIndex, encoder.Encoder)
	}
	if err != nil {
		return fmt.Errorf("unable to init an output stream for encoder %s for input stream #%d: %w", encoder.Encoder, streamIndex, err)
	}

	return nil
}

func (e *Encoder[EF]) initEncoderFor(
	ctx context.Context,
	streamIndex int,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	frameSource frame.Source,
) (_err error) {
	logger.Debugf(ctx, "initEncoderFor(ctx, %d, params, %v, %T)", streamIndex, timeBase, frameSource)
	defer func() {
		logger.Debugf(ctx, "/initEncoderFor(ctx, %d, params, %v, %T): %v", streamIndex, timeBase, frameSource, _err)
	}()

	if timeBase.Num() == 0 {
		return fmt.Errorf("TimeBase must be set")
	}

	var opts []codec.Option
	if frameSource != nil {
		// TODO: use PipelineSideData to pass the decoder getter
		if getDecoderer, ok := frameSource.(codec.GetDecoderer); ok {
			logger.Debugf(ctx, "the frame source implements codec.GetDecoderer: %T", frameSource)
			opts = append(opts, codec.EncoderFactoryOptionGetDecoderer{GetDecoderer: getDecoderer})
		} else {
			logger.Debugf(ctx, "the frame source does not implement codec.GetDecoderer: %T", frameSource)
		}
	} else {
		logger.Debugf(ctx, "the frame source is nil")
		opts = append(opts, codec.EncoderFactoryOptionOnlyDummy{OnlyDummy: true})
	}

	encoderInstance, err := e.EncoderFactory.NewEncoder(ctx, params, timeBase, opts...)
	if err != nil {
		return fmt.Errorf("cannot initialize an encoder for stream %d: %w", streamIndex, err)
	}
	if !codec.IsDummyEncoder(encoderInstance) && encoderInstance.CodecContext(ctx) == nil {
		return fmt.Errorf("the encoder factory produced an encoder %T with nil CodecContext", encoderInstance)
	}

	encoder := &streamEncoder{Encoder: encoderInstance, EncoderConfig: &e.EncoderConfig}
	e.encoders[streamIndex] = encoder
	if e.forceNextKeyFrame.Load() && params.MediaType() == astiav.MediaTypeVideo {
		if codec.IsEncoderCopy(encoderInstance) {
			logger.Debugf(ctx, "not applying pending force-next-key-frame to a copy encoder")
			return nil
		}
		if err := encoderInstance.SetForceNextKeyFrame(ctx, true); err != nil {
			return fmt.Errorf("unable to apply pending force-next-key-frame to encoder for stream %d: %w", streamIndex, err)
		}
	}
	return nil
}

func (e *Encoder[EF]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(e)
}

func (e *Encoder[EF]) String() string {
	return fmt.Sprintf("Encoder(%s)", e.EncoderFactory)
}

func (e *Encoder[EF]) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

type ErrNotCopyEncoder struct{}

func (ErrNotCopyEncoder) Error() string {
	return "one cannot send undecoded packets via an encoder; it is required to decode them first and send as raw frames"
}

func (e *Encoder[EF]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	pkt, frame := input.Unwrap()
	switch {
	case pkt != nil:
		return e.sendPacket(ctx, *pkt, outputCh)
	case frame != nil:
		return e.sendFrame(ctx, *frame, outputCh)
	default:
		return types.ErrUnexpectedInputType{}
	}
}

func (e *Encoder[EF]) sendPacket(
	ctx context.Context,
	input packet.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	ctx = belt.WithField(ctx, "encoder", e)
	logger.Tracef(ctx, "sendPacket: %s", input.GetMediaType())
	defer func() { logger.Tracef(ctx, "/sendPacket: %s: %v", input.GetMediaType(), _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}
	ctx = belt.WithField(ctx, "mode", "packet")
	streamEncoder, err := xsync.DoR2(xsync.WithNoLogging(ctx, true), &e.Locker, func() (*streamEncoder, error) {
		streamEncoder := e.encoders[input.GetStreamIndex()]
		logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), streamEncoder)
		if streamEncoder == nil {
			if stream := input.GetStream(); stream != nil {
				stream.AvgFrameRate()
			}
			logger.Debugf(ctx, "an encoder is not initialized, yet")
			err := e.initEncoderAndOutputFor(
				ctx,
				input.GetStreamIndex(),
				input.GetCodecParameters(),
				input.GetTimeBase(),
				nil,
			)
			switch {
			case err == nil:
			case errors.As(err, &codec.ErrNotDummy{}):
				return nil, ErrNotCopyEncoder{}
			default:
				return nil, fmt.Errorf("unable to update outputs (packet): %w", err)
			}
			streamEncoder = e.encoders[input.GetStreamIndex()]
		}
		assert(ctx, streamEncoder != nil)
		return streamEncoder, nil
	})
	if err != nil {
		return fmt.Errorf("unable to get the encoder for stream index %d: %w", input.GetStreamIndex(), err)
	}

	ctx = belt.WithField(ctx, "encoder", streamEncoder.Encoder)

	if !codec.IsEncoderCopy(streamEncoder.Encoder) {
		return ErrNotCopyEncoder{}
	}

	if encoderDebug {
		if input.Packet.Duration() <= 0 {
			logger.Errorf(ctx, "input packet has no duration set; pos:%d; time_base:%v; stream duration: %v", input.Pos(), input.GetStream().TimeBase(), input.GetStream().Duration())
		}
	}

	outputStream := xsync.DoR1(ctx, &e.Locker, func() *astiav.Stream {
		outputStream := e.outputStreams[input.GetStreamIndex()]
		assert(ctx, outputStream != nil, "outputStream != nil")
		return outputStream
	})

	assert(ctx, outputStream.CodecParameters().MediaType() == input.GetMediaType(), outputStream.CodecParameters().MediaType(), input.GetMediaType())
	pkt := packet.CloneAsReferenced(input.Packet)
	pkt.SetStreamIndex(outputStream.Index())
	err = e.send(ctx, pkt, input.PipelineSideData, outputStream, outputCh)
	if err != nil {
		return fmt.Errorf("unable to send a packet: %w", err)
	}
	return nil
}

func (e *Encoder[EF]) SetForceNextKeyFrame(
	ctx context.Context,
	v bool,
) error {
	logger.Debugf(ctx, "SetForceNextKeyFrame: %v", v)
	e.forceNextKeyFrame.Store(v)
	var errs []error
	e.Locker.Do(xsync.WithNoLogging(ctx, true), func() {
		for _, encoder := range e.encoders {
			if err := encoder.Encoder.SetForceNextKeyFrame(ctx, v); err != nil {
				errs = append(errs, fmt.Errorf("unable to set force next key frame on encoder: %w", err))
			}
		}
	})
	return errors.Join(errs...)
}

func (e *Encoder[EF]) sendFrame(
	ctx context.Context,
	input frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	ctx = belt.WithField(ctx, "encoder", e)
	ctx = belt.WithField(ctx, "mode", "frame")
	ctx = belt.WithField(ctx, "media_type", input.GetMediaType())
	ctx = belt.WithField(ctx, "stream_index", input.GetStreamIndex())
	ctx = belt.WithField(ctx, "is_key", input.Flags().Has(astiav.FrameFlagKey))
	ctx = xsync.WithNoLogging(ctx, true)

	logger.Tracef(ctx, "sendFrame")
	defer func() { logger.Tracef(ctx, "/sendFrame: %v", _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}

	streamEncoder, err := xsync.DoR2(ctx, &e.Locker, func() (*streamEncoder, error) {
		streamEncoder := e.encoders[input.GetStreamIndex()]
		logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), streamEncoder)
		if streamEncoder == nil {
			logger.Debugf(ctx, "an encoder is not initialized, yet")
			err := e.initEncoderAndOutputFor(
				ctx,
				input.StreamIndex,
				fixCodecParameters(ctx, input.CodecParameters),
				input.GetTimeBase(),
				input.Source,
			)
			if err != nil {
				return nil, fmt.Errorf("unable to update outputs (frame): %w", err)
			}
			streamEncoder = e.encoders[input.GetStreamIndex()]
			if encoderWriteHeaderOnFinishedGettingStreams && len(e.encoders) == input.StreamsCount {
				logger.Debugf(ctx, "writing the header")
				err := e.outputFormatContext.WriteHeader(nil)
				if err != nil {
					return nil, fmt.Errorf("unable to write header: %w", err)
				}
				e.headerIsWritten = true
			}
		}
		assert(ctx, streamEncoder != nil)

		ctx = belt.WithField(ctx, "encoder", streamEncoder)

		if encoderDebug {
			logger.Tracef(ctx, "input frame: %s: %s: dur:%d; res:%dx%d, samples:%d", input.GetMediaType(), input.CodecParameters.CodecID(), input.Frame.Duration(), input.Frame.Width(), input.Frame.Height(), input.Frame.NbSamples())
		}

		if encoderWriteHeaderOnFinishedGettingStreams && !e.headerIsWritten && len(e.encoders) == input.StreamsCount {
			logger.Debugf(ctx, "writing the header")
			err := e.outputFormatContext.WriteHeader(nil)
			if err != nil {
				return nil, fmt.Errorf("unable to write header: %w", err)
			}
			e.headerIsWritten = true
		}

		return streamEncoder, nil
	})
	if err != nil {
		return fmt.Errorf("unable to get the encoder for stream index %d: %w", input.GetStreamIndex(), err)
	}

	if codec.IsEncoderRaw(streamEncoder.Encoder) {
		var err error
		select {
		case <-e.ClosureSignaler.CloseChan():
			err = io.ErrClosedPipe
		case <-ctx.Done():
			err = ctx.Err()
		case outputCh <- packetorframe.OutputUnion{
			Frame: ptr(frame.BuildOutput(
				frame.CloneAsReferenced(input.Frame),
				input.StreamInfo,
			)),
		}:
		}
		return err
	}

	if codec.IsEncoderCopy(streamEncoder.Encoder) {
		return codec.ErrCopyEncoder{}
	}

	outputStream := xsync.DoR1(ctx, &e.Locker, func() *astiav.Stream {
		outputStream := e.outputStreams[input.GetStreamIndex()]
		assert(ctx, outputStream != nil, "outputStream != nil")
		return outputStream
	})

	// The codec-parameters re-publish (Bug 6.2) used to live here, but
	// it deadlocked: streamEncoder.Encoder.LockDo acquires the
	// non-reentrant codec lock, and streamEncoder.Encoder.ToCodecParameters
	// (inherited from Codec) re-acquires the same lock. The deadlock
	// stalled the encoder hot path; downstream queues filled, route
	// consumer-detach fired, audio dropped.
	//
	// The republish now runs from inside the drain callback (see
	// republishCodecParamsIfStale called from e.drain), where the
	// encoder is *already* locked and we can write to outputStream's
	// CodecParameters via the locked codec context without further
	// lock acquisition.

	if encoderDebug {
		if encoder, ok := streamEncoder.Encoder.(interface {
			SanityCheck(ctx context.Context) error
		}); ok {
			if err := encoder.SanityCheck(ctx); err != nil {
				return fmt.Errorf("encoder sanity check failed: %w", err)
			}
		}
	}

	outputMediaType := outputStream.CodecParameters().MediaType()
	if encoderExtraDefensive {
		inputMediaType := input.GetMediaType()
		encoderMediaType := streamEncoder.Encoder.MediaType(ctx)
		assert(ctx, inputMediaType == encoderMediaType, inputMediaType, encoderMediaType)
		assert(ctx, outputMediaType == encoderMediaType, outputMediaType, encoderMediaType)

		if isEmptyFrame(ctx, input) {
			logger.Errorf(ctx, "the input frame is empty; dropping it")
			return nil
		}
	}

	err = streamEncoder.Encoder.LockDo(ctx, func(ctx context.Context, encoder codec.Encoder) error {
		streamEncoder := streamEncoder.withLockedEncoder(encoder)
		fittedFrames, err := streamEncoder.fitFrameForEncoding(ctx, input)
		if err != nil {
			return fmt.Errorf("unable to fit the frame for encoding: %w", err)
		}

		if len(fittedFrames) == 0 {
			// fitFrameForEncoding can return zero frames during
			// resampler warmup or PCM format mismatch resolution.
			// Logged at Warnf (not Tracef) so cascade-internal audio
			// drops are visible at production log levels. The file
			// qualifier "kernel/encoder.go:fitFrameForEncoding" lets
			// operators grep across multi-package log streams to find
			// the exact origin of the drop. mediaType disambiguates
			// the audio-vs-video stream that dropped the frame.
			logger.Warnf(ctx, "kernel/encoder.go:fitFrameForEncoding: frame dropped (mediaType=%s; resampler warmup or PCM format mismatch)", streamEncoder.Encoder.MediaType(ctx))
			return nil
		}

		frameInfo := FrameInfo{
			PTS:         input.Pts(),
			DTS:         input.PktDts(),
			Duration:    input.Frame.Duration(),
			StreamIndex: input.StreamIndex,
			TimeBase:    input.GetTimeBase(),
			FrameFlags:  input.Flags(),
			PictureType: input.Frame.PictureType(),
		}

		if encoder.Codec(ctx) == nil {
			logger.Errorf(ctx, "the encoder is closed; dropping the frame")
			return nil
		}
		for _, fittedFrame := range fittedFrames {
			if encoderDebug {
				logger.Tracef(ctx, "fitted frame: dur:%d, dts:%d, pts:%d", fittedFrame.Duration(), fittedFrame.PktDts(), input.Frame.Pts())
			}
			if fittedFrame.Pts() != consts.NoPTSValue {
				frameInfo.PTS = fittedFrame.Pts()
			}
			if fittedFrame.PktDts() != consts.NoPTSValue {
				frameInfo.DTS = fittedFrame.PktDts()
			}
			if fittedFrame.Duration() > 0 {
				frameInfo.Duration = fittedFrame.Duration()
			}

			fittedFrame.SetOpaque(frameInfo.Bytes())
			err := sendFrameWithDrainRetry(
				ctx,
				streamEncoder.streamEncoder,
				fittedFrame,
				frameInfo,
				outputMediaType,
				encoder.SendFrame,
				func(ctx context.Context) error {
					return e.drain(
						ctx,
						outputCh,
						encoder.Drain,
						outputStream,
						frameInfo,
						streamEncoder.streamEncoder,
					)
				},
			)
			if err != nil {
				return err
			}
		}

		// TODO: research: what's better: send all inputs and then read all outputs, or read after each send?
		// For now we do the batching approach.
		err = e.drain(
			ctx,
			outputCh,
			encoder.Drain,
			outputStream,
			frameInfo,
			streamEncoder.streamEncoder,
		)
		if err != nil {
			return fmt.Errorf("unable to drain: %w", err)
		}

		return nil
	})
	return e.handleEncoderStall(ctx, streamEncoder, err)
}

// sendFrameWithDrainRetry implements the bounded EAGAIN-drain retry
// loop for codec.Encoder.SendFrame. The previous unbounded form
// (`for { SendFrame; if EAGAIN { drain; continue } }`) wedged the
// encoder hot path forever when SendFrame entered a genuine stall —
// e.g. av1_mediacodec output queue not advancing — because Drain
// produced no packets, so EAGAIN persisted across every retry.
//
// Bound: at most sendFrameMaxRetries drain-retry cycles per fitted
// frame. We additionally short-circuit a retry when a Drain pass
// produced no new packets (drainPacketCount unchanged): if Drain made
// no progress, looping again can't change SendFrame's answer.
//
// On exhausted retries the caller (sendFrame via handleEncoderStall)
// observes errEncoderStalled, increments consecutiveStalls, and may
// escalate to a full Reinit. Any other error is wrapped and surfaced.
//
// On a successful SendFrame, FrameInfoFIFO is appended and
// consecutiveStalls is reset to 0.
//
// Function-typed sendFrame / drain dependencies make the loop directly
// unit-testable without standing up a real codec — see
// encoder_stall_test.go.
func sendFrameWithDrainRetry(
	ctx context.Context,
	se *streamEncoder,
	fittedFrame *astiav.Frame,
	frameInfo FrameInfo,
	outputMediaType astiav.MediaType,
	sendFrame func(context.Context, *astiav.Frame) error,
	drain func(context.Context) error,
) error {
	retries := 0
	for {
		err := sendFrame(ctx, fittedFrame)
		switch {
		case err == nil:
			se.FrameInfoFIFO = append(se.FrameInfoFIFO, frameInfo)
			if encoderDebug {
				logger.Tracef(ctx, "FIFO push: dts:%d pts:%d tb:%v fifo_len:%d",
					frameInfo.DTS, frameInfo.PTS, frameInfo.TimeBase, len(se.FrameInfoFIFO))
			}
			se.consecutiveStalls.Store(0)
			// silent-consume watchdog: count input frames accepted
			// without a corresponding drain output. Reset by the drain
			// callback on every emitted packet.
			se.framesInSinceLastPacket.Add(1)
			return nil
		case errors.Is(err, astiav.ErrEagain):
			if retries >= sendFrameMaxRetries {
				stalls := se.consecutiveStalls.Add(1)
				logger.Warnf(ctx, "encoder.SendFrame: EAGAIN after %d drains; bailing on frame pts=%d (stall_count=%d)",
					retries, fittedFrame.Pts(), stalls)
				return errEncoderStalled
			}
			retries++
			pktsBefore := se.drainPacketCount.Load()
			logger.Tracef(ctx, "encoder.SendFrame(): EAGAIN; draining and retrying (retry %d/%d)", retries, sendFrameMaxRetries)
			if derr := drain(ctx); derr != nil {
				return fmt.Errorf("unable to drain: %w", derr)
			}
			if se.drainPacketCount.Load() == pktsBefore {
				// Drain produced no packets -- another SendFrame call
				// would just hit EAGAIN again. Skip straight to the
				// next retry-budget check.
				continue
			}
			// Drain produced packets: the encoder made progress, so
			// reset the local retry budget. We still cap the *total*
			// retries via sendFrameMaxRetries above; resetting only
			// the no-progress probe means a healthy "needs draining
			// every frame" codec doesn't get false-stalled.
			retries = 0
			continue
		default:
			return fmt.Errorf("unable to send a %s frame to the encoder: %w", outputMediaType, err)
		}
	}
}

// handleEncoderStall, handleSilentConsumeStall, and the shared
// escalateStallReinit recovery helper live in encoder_stall.go.

func isEmptyFrame(
	ctx context.Context,
	input frame.Input,
) (_ret bool) {
	defer func() { logger.Tracef(ctx, "/isEmptyFrame: %v", _ret) }()
	if input.Frame == nil {
		return true
	}
	switch input.GetMediaType() {
	case astiav.MediaTypeVideo:
		if input.Frame.Width() <= 0 || input.Frame.Height() <= 0 {
			return true
		}
	case astiav.MediaTypeAudio:
		if input.Frame.NbSamples() <= 0 {
			return true
		}
	}
	return false
}

func (e *Encoder[EF]) drain(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
	encoderDrainFn func(context.Context, codec.CallbackPacketReceiver) error,
	outputStream *astiav.Stream,
	frameInfo FrameInfo,
	streamEncoder *streamEncoder,
) (_err error) {
	logger.Tracef(ctx, "drain")
	defer func() { logger.Tracef(ctx, "/drain: %v", _err) }()

	frameInfoFIFO := &streamEncoder.FrameInfoFIFO
	packetCount := 0
	return encoderDrainFn(ctx, func(
		ctx context.Context,
		encoder *codec.EncoderFullLocked,
		caps astiav.CodecCapabilities,
		pkt *astiav.Packet,
	) error {
		packetCount++
		// Bump per-streamEncoder lifetime drain packet count. The
		// bounded SendFrame retry loop reads this counter to detect
		// "drain produced no progress" and bail out instead of
		// spinning forever (av1_mediacodec genuine stall).
		streamEncoder.drainPacketCount.Add(1)
		// silent-consume watchdog: a packet emitted means the
		// codec made forward progress, so reset the input-without-output
		// counter. Without this reset the watchdog would false-trigger
		// on encoders with legitimate startup delay (e.g. B-frame
		// look-ahead) once they begin producing output.
		streamEncoder.framesInSinceLastPacket.Store(0)
		opaque := pkt.Opaque()
		logger.Tracef(ctx, "encoder.ReceivePacket(): got the %dth %s packet, resulting size: %d (pts: %d); opaque size: %d", packetCount, outputStream.CodecParameters().MediaType(), pkt.Size(), pkt.Pts(), len(opaque))

		// Hardware encoders (e.g. h264_rkmpp) may not populate
		// AVCodecContext.extradata until the first frame is encoded.
		// Update the output stream's codec parameters so that
		// downstream consumers (FLV muxer, RTMP servers) receive
		// correct SPS/PPS in the stream header.
		if outputStream.CodecParameters().ExtraData() == nil {
			if cc := encoder.CodecContext(ctx); cc != nil {
				if err := cc.ToCodecParameters(outputStream.CodecParameters()); err != nil {
					logger.Warnf(ctx, "unable to update output stream codec parameters from encoder: %v", err)
				} else {
					logger.Debugf(ctx, "updated output stream codec parameters from encoder (extradata was empty)")
				}
			}
		}

		// Bug 6.2: when the audio encoder was reinitialized to refresh
		// extradata after a resampler rebuild (reinitEncoderForResamplerRebuild),
		// LastInitTS was reset and the encoder's InitTS advanced. Republish
		// the now-fresh codec parameters (including AAC ASC) onto outputStream
		// so downstream consumers see the correct sample_rate / channel layout
		// after a mediamtx interrupt+reconnect. The republish runs here, inside
		// the drain callback, where the encoder is already locked — calling
		// encoder.ToCodecParameters at the encoder hot path would re-acquire
		// the same non-reentrant lock and deadlock the encoder (the regression
		// in commit 9f07679, reverted in e99df5e).
		if enableStreamCodecParametersUpdates {
			if cc := encoder.CodecContext(ctx); cc != nil {
				republishCodecParamsIfStale(
					ctx,
					streamEncoder,
					outputStream,
					encoder.InitTS,
					cc.ToCodecParameters,
				)
			}
		}

		pkt.SetStreamIndex(outputStream.Index())

		if opaque != nil {
			frameInfoPtr := FrameInfoFromBytes(opaque)
			if frameInfoPtr == nil {
				return fmt.Errorf("unable to parse the frame info from the packet opaque data: %v", opaque)
			}
			frameInfo = *frameInfoPtr
		}

		if caps&astiav.CodecCapabilityDr1 != 0 || encoderForceCopyTime {
			logger.Tracef(ctx, "setting manually the packet timestamps")
			if pkt.Duration() <= 0 {
				pkt.SetDuration(frameInfo.Duration)
			}
			if pkt.Duration() <= 0 {
				// When the encoder doesn't round-trip opaque data (e.g.
				// hevc_mediacodec) and AVFilter strips frame duration,
				// both pkt.Duration() and frameInfo.Duration are 0.
				// Compute duration from the codec's framerate.
				if fps := outputStream.CodecParameters().FrameRate(); fps.Num() > 0 && fps.Den() > 0 {
					dur := astiav.RescaleQ(
						1,
						astiav.NewRational(fps.Den(), fps.Num()),
						outputStream.TimeBase(),
					)
					if dur > 0 {
						pkt.SetDuration(dur)
						logger.Tracef(ctx, "encoder drain: computed duration %d from framerate %v (time_base:%v)",
							dur, fps, outputStream.TimeBase())
					}
				}
			}
			switch {
			case opaque != nil:
				// Opaque data round-tripped: use it for per-packet timestamps.
				pkt.SetDts(frameInfo.DTS)
				pkt.SetPts(frameInfo.PTS)
				pkt.RescaleTs(frameInfo.TimeBase, outputStream.TimeBase())
			case frameInfoFIFO != nil && len(*frameInfoFIFO) > 0:
				// Opaque didn't round-trip (e.g. AAC encoder with 'delay' but
				// without 'encoder_reordered_opaque'). Pop the oldest FrameInfo
				// from the FIFO — since non-reordering codecs (like AAC) output
				// packets in the same order as input frames (just delayed), the
				// FIFO gives the correct per-packet timestamps.
				fi := (*frameInfoFIFO)[0]
				*frameInfoFIFO = (*frameInfoFIFO)[1:]
				pkt.SetDts(fi.DTS)
				pkt.SetPts(fi.PTS)
				if pkt.Duration() <= 0 {
					pkt.SetDuration(fi.Duration)
				}
				logger.Tracef(ctx, "encoder drain: FIFO pop (dts:%d, pts:%d, tb:%v, fifo_remaining:%d)",
					fi.DTS, fi.PTS, fi.TimeBase, len(*frameInfoFIFO))
				pkt.RescaleTs(fi.TimeBase, outputStream.TimeBase())
			default:
				// No opaque and no FIFO -- fall back to frameInfo parameter
				// (last resort, may be inaccurate for delayed codecs).
				logger.Warnf(ctx, "encoder drain: FIFO empty and no opaque; using fallback frameInfo (dts:%d, pts:%d, tb:%v, pkt_count:%d)",
					frameInfo.DTS, frameInfo.PTS, frameInfo.TimeBase, packetCount)
				pkt.SetDts(frameInfo.DTS)
				pkt.SetPts(frameInfo.PTS)
				pkt.RescaleTs(frameInfo.TimeBase, outputStream.TimeBase())
			}
		}

		// pkt.SetPos(-1) // <- TODO: should this happen? why?
		if pkt.Dts() > pkt.Pts() && pkt.Dts() != consts.NoPTSValue && pkt.Pts() != consts.NoPTSValue && (frameInfo.PictureType != astiav.PictureTypeB) {
			if encoderDTSHigherPTSCorrect {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) correcting DTS to %d (pict-type: 0x%02X)", pkt.Dts(), pkt.Pts(), pkt.Pts(), int(frameInfo.PictureType))
				pkt.SetDts(pkt.Pts())
			} else {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) skipping the packet (pict-type: 0x%02X)", pkt.Dts(), pkt.Pts(), int(frameInfo.PictureType))
				packet.Pool.Put(pkt)
				return nil
			}
		}

		err := e.send(ctx, pkt, []any{frameInfo}, outputStream, outputCh)
		if err != nil {
			return fmt.Errorf("unable to send a packet: %w", err)
		}

		return nil
	})
}

type streamEncoderLocked struct {
	codec.Encoder
	*streamEncoder
}

func (e *streamEncoder) withLockedEncoder(
	encoder codec.Encoder,
) *streamEncoderLocked {
	return &streamEncoderLocked{
		Encoder:       encoder,
		streamEncoder: e,
	}
}

func (e *streamEncoderLocked) fitFrameForEncoding(
	ctx context.Context,
	input frame.Input,
) (fittedFrames []*astiav.Frame, _err error) {
	logger.Tracef(ctx, "fitFrameForEncoding: %s", e.MediaType(ctx))
	defer func() { logger.Tracef(ctx, "/fitFrameForEncoding: %s: %v %v", e.MediaType(ctx), fittedFrames, _err) }()

	switch e.MediaType(ctx) {
	case astiav.MediaTypeVideo:
		res := e.GetResolution(ctx)
		if res == nil {
			return nil, fmt.Errorf("unable to get the resolution from the encoder")
		}
		encoderPixelFormat := e.CodecContext(ctx).PixelFormat()
		hwFramesCtx := e.Encoder.HardwareFramesContext(ctx)
		// For HwFramesCtx-mode encoders, the codec context advertises a
		// HW pixfmt; the encoder's SendFrame will upload SW frames via
		// av_hwframe_transfer_data. Normalise to the SW upload format
		// (sw_format from hw_frames_ctx, or NV12 fallback) so a SW
		// input frame in that format can pass through unchanged. See
		// encoder_scaler_pixfmt.go.
		passthroughPixelFormat := selectScaledFramePixelFormat(encoderPixelFormat, hwFramesCtx)
		if encoderDebug {
			logger.Tracef(ctx, "input frame: %dx%d/%s (%s); encoder resolution: %s, encoder pixel format: %s, passthrough pixel format: %s", input.Frame.Width(), input.Frame.Height(), input.PixelFormat(), input.CodecParameters.CodecID(), res, encoderPixelFormat, passthroughPixelFormat)
		}
		if !encoderRescaleSameResolution {
			if shouldBypassScaler(
				input.PixelFormat(), input.Frame.Width(), input.Frame.Height(),
				encoderPixelFormat, int(res.Width), int(res.Height),
				hwFramesCtx,
			) {
				logger.Tracef(ctx, "frame %dx%d/%s matches encoder; passing through without conversion", input.Frame.Width(), input.Frame.Height(), input.PixelFormat())
				return []*astiav.Frame{input.Frame}, nil
			}
		}
		logger.Tracef(ctx, "scaling the frame from %dx%d/%s to %s/%s", input.Frame.Width(), input.Frame.Height(), input.PixelFormat(), res, e.CodecContext(ctx).PixelFormat())
		scaledFrame, err := e.getScaledFrame(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("unable to scale the frame from %dx%d/%s to %s/%s: %w", input.Frame.Width(), input.Frame.Height(), input.PixelFormat(), res, e.CodecContext(ctx).PixelFormat(), err)
		}
		if encoderCopyTimeAfterScaling {
			scaledFrame.SetPts(input.Frame.Pts())
			scaledFrame.SetPktDts(input.Frame.PktDts())
			scaledFrame.SetDuration(input.Frame.Duration())
		}
		return []*astiav.Frame{scaledFrame}, nil
	case astiav.MediaTypeAudio:
		inPCMFmt := getPCMAudioFormatFromFrame(ctx, input.Frame)
		if inPCMFmt == nil {
			return nil, fmt.Errorf("unable to get PCM audio format from the input frame")
		}
		encPCMFmt := e.GetPCMAudioFormat(ctx)
		if encPCMFmt == nil {
			return []*astiav.Frame{input.Frame}, nil
		}
		if encPCMFmt.Equal(*inPCMFmt) {
			return []*astiav.Frame{input.Frame}, nil
		}
		resampledFrames, err := e.getResampledFrames(ctx, input.Frame, *encPCMFmt)
		if err != nil {
			return nil, fmt.Errorf("unable to resample the audio frame: %w", err)
		}
		// Resampler returns frames without PTS/Duration; restamp them in
		// output-sample-rate units (still expressed in the input timebase
		// the encoder downstream rescales from) so downstream FrameInfo
		// reflects the actual output sample count, not the input frame's
		// span. Without this, cross-rate resampling produces a per-frame
		// DTS gap of (1 - inRate/outRate) and ~8% audio-continuity loss
		// for 44100->48000 (or worse for larger ratios). Same-rate paths
		// (channel layout/format-only resampling) are unaffected because
		// inPCMFmt.SampleRate == encPCMFmt.SampleRate skips this branch.
		if len(resampledFrames) > 0 && inPCMFmt.SampleRate != encPCMFmt.SampleRate {
			inputTB := input.GetTimeBase()
			outRateTB := astiav.NewRational(1, encPCMFmt.SampleRate)
			if !e.audioResampleHaveOutAnchor {
				if input.Frame.Pts() != consts.NoPTSValue {
					e.audioResampleNextOutPTS = astiav.RescaleQ(input.Frame.Pts(), inputTB, outRateTB)
				}
				e.audioResampleHaveOutAnchor = true
			}
			for _, rf := range resampledFrames {
				nb := int64(rf.NbSamples())
				if nb <= 0 {
					continue
				}
				ptsIn := astiav.RescaleQ(e.audioResampleNextOutPTS, outRateTB, inputTB)
				durIn := astiav.RescaleQ(nb, outRateTB, inputTB)
				rf.SetPts(ptsIn)
				rf.SetPktDts(ptsIn)
				rf.SetDuration(durIn)
				e.audioResampleNextOutPTS += nb
			}
		}
		return resampledFrames, nil
	default:
		logger.Debugf(ctx, "unsupported media type: %s", e.MediaType(ctx))
		return []*astiav.Frame{input.Frame}, nil
	}
}

// getPCMAudioFormatFromFrame, streamEncoderLocked.getResampledFrames,
// and streamEncoderLocked.prepareResampler live in encoder_resample.go.

// republishCodecParamsIfStale propagates the encoder's freshly-
// regenerated codec parameters (including AAC ASC / extradata)
// onto outputStream whenever encoderInitTS is newer than
// streamEncoder.LastInitTS. The writer argument is the caller-
// supplied function that actually copies parameters from the locked
// codec context to the output stream's CodecParameters; the helper
// itself does not touch the encoder lock, so it is safe to call from
// inside an already-locked encoder context (drain callback).
//
// Why a helper instead of an inline block on the encoder hot path:
// the previous incarnation of this code (commit 9f07679, reverted in
// e99df5e) lived inside streamEncoder.Encoder.LockDo and called
// streamEncoder.Encoder.ToCodecParameters, which re-acquired the
// non-reentrant codec lock and deadlocked the encoder hot path on
// real EncoderFull instances; queues filled, route consumer-detach
// fired, audio dropped. The fix runs the publish from inside
// drain's callback (where the codec context is already locked),
// using a writer (cc.ToCodecParameters) that does no further locking.
//
// Returning an error from the writer is treated as "do not advance
// LastInitTS" so the next packet retries the publish. The Warnf is
// rate-limited via streamEncoder.lastCodecParamsRepublishWarnAt to at
// most once per codecParamsRepublishWarnBackoff (1s) — without that,
// a writer that fails on every drained packet at AAC frame rate would
// flood the log at ~50 messages/second.
//
// Pure helper around now() (no clock seam): the rate-limit is monotonic
// against time.Now(), and the test suite already uses small-N invocations
// rather than wall-time advancement to exercise the success vs failure
// branches, so a clock seam would be additional surface without test
// benefit.
func republishCodecParamsIfStale(
	ctx context.Context,
	streamEncoder *streamEncoder,
	outputStream *astiav.Stream,
	encoderInitTS time.Time,
	writer func(*astiav.CodecParameters) error,
) {
	if streamEncoder == nil || outputStream == nil || writer == nil {
		return
	}
	if !streamEncoder.LastInitTS.Before(encoderInitTS) {
		return
	}
	logger.Debugf(ctx, "republishing codec parameters (LastInitTS=%v -> %v)", streamEncoder.LastInitTS, encoderInitTS)
	if err := writer(outputStream.CodecParameters()); err != nil {
		now := time.Now()
		if streamEncoder.lastCodecParamsRepublishWarnAt.IsZero() ||
			now.Sub(streamEncoder.lastCodecParamsRepublishWarnAt) >= codecParamsRepublishWarnBackoff {
			logger.Warnf(ctx, "unable to republish codec parameters from encoder to output stream: %v", err)
			streamEncoder.lastCodecParamsRepublishWarnAt = now
		} else {
			logger.Debugf(ctx, "unable to republish codec parameters from encoder to output stream: %v (warn suppressed by %v backoff)", err, codecParamsRepublishWarnBackoff)
		}
		return
	}
	streamEncoder.LastInitTS = encoderInitTS
	// On a successful republish, clear the warn-backoff stamp so the
	// next failure window is observed quickly rather than waiting out
	// the prior backoff.
	streamEncoder.lastCodecParamsRepublishWarnAt = time.Time{}
}

// streamEncoderLocked.reinitEncoderForResamplerRebuild lives in
// encoder_resample.go.

// getScaledFrame produces a frame in the pixel format expected by the
// underlying encoder, performing HW->SW download and/or SW scaling as
// required by the input frame's format and the encoder's target.
//
// IN-PLACE MUTATION CAVEAT: when input.Frame has a HW pixel format but
// no hw_frames_ctx attached (the mediacodec-decoder case), this
// function MAY mutate input.Frame.hw_frames_ctx in place by attaching a
// hw_frames_ctx borrowed from the upstream decoder via input.Source.
// Callers must be OK with this — the frame is consumed downstream
// (scaler reads from frameSrc, then the upstream queue Unrefs) so the
// borrowed context lifetime is bounded by the caller's ownership. A
// wrapping Ref/clone would also work but adds a buffer copy. If a
// caller ever needs to preserve the original frame.hw_frames_ctx across
// this call, it must Clone the frame before invoking getScaledFrame.
func (e *streamEncoderLocked) getScaledFrame(
	ctx context.Context,
	input frame.Input,
) (_ret *astiav.Frame, _err error) {
	logger.Tracef(ctx, "getScaledFrame")
	defer func() { logger.Tracef(ctx, "/getScaledFrame: %v", _err) }()

	frameSrc := input.Frame

	// Transfer hardware frames to software before preparing the scaler.
	// Decoders using CUDA hw_device_ctx produce frames with cuda pixel format
	// and hw_frames_ctx set. Software scalers cannot handle hardware pixel formats,
	// so we must convert to software first.
	//
	// Two distinct cases trigger the download:
	//   (1) frame has hw_frames_ctx attached -- av_hwframe_transfer_data
	//       reads dims/sw_format from it directly. CUDA path works this way.
	//   (2) frame has a HW pixfmt but no hw_frames_ctx attached -- happens
	//       in practice with mediacodec decoder output on Android:
	//       the frame's pix_fmt is AV_PIX_FMT_MEDIACODEC yet hw_frames_ctx
	//       is unset. To run av_hwframe_transfer_data we must borrow the
	//       hw_frames_ctx from the upstream decoder via input.Source's
	//       GetDecoderer, attach it to the frame, then transfer.
	//
	// Gate on HW pixfmt directly so case (2) is covered.
	if isHardwarePixelFormat(frameSrc.PixelFormat()) {
		logger.Tracef(ctx, "transferring the frame data from hardware to software (pix_fmt=%s, frame.hw_frames_ctx_attached=%t)",
			frameSrc.PixelFormat(), frameSrc.HardwareFramesContext() != nil)
		// Ensure src has hw_frames_ctx for av_hwframe_transfer_data. If
		// missing, borrow it from the upstream decoder. We mutate the
		// caller's frame in place; a wrapping Ref/clone would also work
		// but adds a buffer copy, and the frame is consumed downstream
		// (scaler reads from frameSrc, then upstream queue Unrefs).
		if frameSrc.HardwareFramesContext() == nil {
			if provider, ok := input.Source.(HardwareSourceFramesContextProvider); ok {
				if dec := provider.GetDecoder(); dec != nil {
					// Lockless read: the encoder is INSIDE its own LockDo
					// here. The locking *Decoder.HardwareFramesContext
					// (promoted from *Codec) would acquire the decoder's
					// locker — which is concurrently held by the
					// decoder's sendPacket path while MediaCodec.SendPacket
					// is CGO-blocked waiting for the encoder to drain its
					// output. See HardwareFramesContextLockless in
					// codec/decoder.go for the full chain and the safety
					// argument.
					if hfc := dec.HardwareFramesContextLockless(); hfc != nil {
						frameSrc.SetHardwareFramesContext(hfc)
						logger.Tracef(ctx, "attached upstream decoder's hw_frames_ctx to src frame")
					} else if frameSrc.PixelFormat() == astiav.PixelFormatMediacodec {
						// FFmpeg's mediacodec decoder operates in Surface
						// mode, which means it produces
						// AV_PIX_FMT_MEDIACODEC frames but never allocates
						// an AVHWFramesContext on its codec context — the
						// borrow above returns nil. Without an HFC,
						// av_hwframe_transfer_data has no buffer pool to
						// download into and we can't reach the SW scaler.
						// Allocate one ad-hoc on the decoder using its
						// existing AVHWDeviceContext and NV12 sw_format
						// (the universal mediacodec SW upload format,
						// validated by mediacodecenc.c copy_frame_to_buffer
						// path). Lazy + per-decoder so it allocates once
						// and is reused for every subsequent frame; freed
						// at decoder close. Lockless via atomic.Pointer
						// CAS — see EnsureLazyHardwareFramesContext doc.
						//
						// Pool size MUST be 0 here: FFmpeg's
						// hwcontext_mediacodec.c does not implement
						// frames_get_buffer, so av_hwframe_ctx_init's
						// pool-prealloc loop returns ENOSYS for any positive
						// size. The HFC is needed only as a metadata carrier
						// (width/height/sw_format) for
						// av_hwframe_transfer_data — see the invariant
						// pinned in codec/codec.go (the SetInitialPoolSize
						// gate honours zero by skipping the call, matching
						// FFmpeg's "no preallocation" behaviour).
						const lazyMediaCodecPoolSize = 0
						lazyHFC, err := dec.EnsureLazyHardwareFramesContext(
							ctx,
							frameSrc.Width(), frameSrc.Height(),
							astiav.PixelFormatNv12,
							lazyMediaCodecPoolSize,
						)
						if err != nil {
							logger.Warnf(ctx,
								"unable to lazy-allocate hw_frames_ctx for mediacodec Surface-mode decoder (%dx%d): %v",
								frameSrc.Width(), frameSrc.Height(), err)
						} else if lazyHFC != nil {
							frameSrc.SetHardwareFramesContext(lazyHFC)
							logger.Tracef(ctx, "attached lazy-allocated mediacodec hw_frames_ctx to src frame")
						}
					}
				}
			}
			if frameSrc.HardwareFramesContext() == nil {
				return nil, fmt.Errorf("frame has HW pixfmt %s but no hw_frames_ctx and upstream decoder has none either; cannot transfer to software",
					frameSrc.PixelFormat())
			}
		}
		sw := astiav.AllocFrame()
		setFinalizerFree(ctx, sw)
		if err := frameSrc.TransferHardwareData(sw); err != nil {
			return nil, fmt.Errorf("unable to transfer the frame data from hardware to software: %w", err)
		}
		frameSrc = sw
		input.Frame = frameSrc
	}

	// The fitFrameForEncoding passthrough at the call site cannot match HW input
	// (its PixelFormat() is `cuda`, not the encoder's sw pixfmt), so we re-check
	// here after the HW->SW transfer. Without this short-circuit, identity
	// NV12->NV12 same-resolution swscale runs and zeroes the UV plane on some
	// libswscale builds (cuvid->nvenc green-frame bug). If encoderRescaleEnableCropping
	// or encoderRescaleEnableTightPacking are ever enabled, revisit this gate.
	if canBypassScaler {
		if dstRes := e.GetResolution(ctx); dstRes != nil {
			// shouldBypassScaler matches against either:
			//   (a) the SW pixfmt the scaler would target -- HwFramesCtx-
			//       mode encoders advertise a HW pixfmt that no SW frame
			//       can equal, so a naive == against CodecContext.PixelFormat
			//       would always run a redundant NV12->NV12 scale.
			//   (b) the encoder's pixfmt directly -- restores HW->HW
			//       passthrough that arm (a) alone misses (e.g. mediacodec
			//       decoder feeding a mediacodec encoder; running
			//       libswscale on a hwaccel source stalls the queue).
			if shouldBypassScaler(
				frameSrc.PixelFormat(), frameSrc.Width(), frameSrc.Height(),
				e.CodecContext(ctx).PixelFormat(), int(dstRes.Width), int(dstRes.Height),
				e.Encoder.HardwareFramesContext(ctx),
			) {
				logger.Tracef(ctx, "frame %dx%d/%s matches encoder; skipping scaler", frameSrc.Width(), frameSrc.Height(), frameSrc.PixelFormat())
				return frameSrc, nil
			}
		}
	}

	err := e.prepareScaler(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("unable to get a scaler: %w", err)
	}

	if encoderRescaleEnableCropping {
		if frameSrc == input.Frame {
			cloned, cloneErr := frame.CloneAsWritable(frameSrc)
			if cloneErr != nil {
				return nil, fmt.Errorf("unable to clone frame as writable: %w", cloneErr)
			}
			frameSrc = cloned
		}
		err = frameSrc.ApplyCropping(0)
		if err != nil {
			return nil, fmt.Errorf("unable to apply cropping: %w", err)
		}
	}

	if encoderRescaleEnableTightPacking {
		if provider, ok := input.Source.(HardwareSourceFramesContextProvider); ok {
			decoder := provider.GetDecoder()
			if strings.HasSuffix(decoder.Codec.Codec(ctx).Name(), "_mediacodec") {
				packed, err := tightPack(ctx, frameSrc)
				if err != nil {
					return nil, fmt.Errorf("unable to tight-pack the frame: %w", err)
				}
				// tightPack returns a Pool.Get'd frame; once the scaler has
				// consumed it, return it to the pool to avoid the slab-alias
				// UAF (see pool/no_raw_pool_put_test.go).
				defer frame.Pool.Put(packed)
				frameSrc = packed
			}
		}
	}

	err = e.Scaler.ScaleFrame(ctx, frameSrc, e.ScaledFrame)
	if err != nil {
		return nil, fmt.Errorf("unable to scale the frame: %w", err)
	}
	return e.ScaledFrame, nil
}

func tightPack(
	ctx context.Context,
	input *astiav.Frame,
) (packed *astiav.Frame, _err error) {
	logger.Tracef(ctx, "tightPack: %s", input.PixelFormat())
	defer func() { logger.Tracef(ctx, "/tightPack: %s: %v", input.PixelFormat(), _err) }()
	switch input.PixelFormat() {
	case astiav.PixelFormatNv12:
		return tightPackNV12(input)
	default:
		return nil, fmt.Errorf("tight packing is not implemented for pixel format %s", input.PixelFormat())
	}
}

// tightPackNV12 copies only the visible WxH and discards any padded columns/rows.
// align=1 => linesize(Y)=W, linesize(UV)=W for NV12.
func tightPackNV12(src *astiav.Frame) (_packed *astiav.Frame, _err error) {
	const align = 1
	srcSize, err := src.ImageBufferSize(align)
	if err != nil {
		return nil, fmt.Errorf("unable to compute image buffer size: %w", err)
	}

	tmp := make([]byte, srcSize)
	if _, err := src.ImageCopyToBuffer(tmp, align); err != nil {
		return nil, fmt.Errorf("unable to copy image to buffer: %w", err)
	}

	packed := frame.Pool.Get()
	// Return packed to the pool on any failure between Get and successful
	// return so we don't drop a pooled frame to GC (slab-alias UAF).
	defer func() {
		if _err != nil {
			frame.Pool.Put(packed)
		}
	}()
	if err := packed.MakeWritable(); err != nil {
		return nil, fmt.Errorf("unable to make frame writable: %w", err)
	}
	packed.SetPixelFormat(src.PixelFormat())
	packed.SetWidth(src.Width())
	packed.SetHeight(src.Height())
	if err := packed.AllocBuffer(align); err != nil {
		return nil, fmt.Errorf("unable to allocate buffer: %w", err)
	}
	if err := packed.Data().SetBytes(tmp, align); err != nil {
		return nil, fmt.Errorf("unable to set frame data: %w", err)
	}

	packed.SetPts(src.Pts())
	packed.SetSampleAspectRatio(src.SampleAspectRatio())
	packed.SetColorRange(src.ColorRange())
	packed.SetColorSpace(src.ColorSpace())
	return packed, nil
}

func (e *streamEncoderLocked) prepareScaler(
	ctx context.Context,
	input frame.Input,
) (_err error) {
	inputResolution := input.GetResolution()

	outputResolution := e.GetResolution(ctx)
	logger.Tracef(ctx, "prepareScaler: %v/%v->%v/%v", inputResolution, input.PixelFormat(), outputResolution, e.CodecContext(ctx).PixelFormat())
	defer func() {
		logger.Tracef(ctx, "/prepareScaler: %v/%v->%v/%v: %v", inputResolution, input.PixelFormat(), outputResolution, e.CodecContext(ctx).PixelFormat(), _err)
	}()

	if outputResolution == nil {
		return fmt.Errorf("unable to get the resolution from the encoder")
	}

	if e.Scaler != nil {
		if e.Scaler.SourceResolution() == inputResolution && e.Scaler.DestinationResolution() == *outputResolution && input.PixelFormat() == e.Scaler.SourcePixelFormat() && e.CodecContext(ctx).PixelFormat() == e.Scaler.DestinationPixelFormat() {
			logger.Tracef(ctx, "reusing the scaler")
			return nil
		}
		if err := e.Scaler.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close the scaler: %v", err)
		}
	}

	// Free the previous frame before allocating a new one to avoid leaking
	// C-heap memory: Go's GC doesn't account for the C-side buffer size, so
	// relying on the finalizer alone causes unbounded growth on resolution changes.
	if e.ScaledFrame != nil {
		e.ScaledFrame.Free()
	}

	// Pick the destination pixel format. When the encoder is in
	// HwFramesCtx mode (av1_mediacodec, hevc_mediacodec on Android, or
	// NVENC reusing an upstream cuvid hw_frames_ctx) CodecContext
	// reports a HW pixfmt that av_frame_get_buffer cannot allocate
	// for; the scaler must produce a SW frame in the hw_frames_ctx
	// sw_format and let EncoderFullLocked.SendFrame upload it via
	// av_hwframe_transfer_data. See encoder_scaler_pixfmt.go.
	scaledFramePixFmt := selectScaledFramePixelFormat(
		e.CodecContext(ctx).PixelFormat(),
		e.Encoder.HardwareFramesContext(ctx),
	)

	e.ScaledFrame = astiav.AllocFrame()
	setFinalizerFree(ctx, e.ScaledFrame)
	e.ScaledFrame.SetWidth(int(outputResolution.Width))
	e.ScaledFrame.SetHeight(int(outputResolution.Height))
	e.ScaledFrame.SetPixelFormat(scaledFramePixFmt)
	if err := e.ScaledFrame.AllocBuffer(0); err != nil {
		return fmt.Errorf("unable to allocate a buffer for the scaled frame (dst pixfmt=%s, encoder pixfmt=%s): %w",
			scaledFramePixFmt, e.CodecContext(ctx).PixelFormat(), err)
	}

	// Validate the source/destination descriptor pair BEFORE
	// sws_getContext so a NULL return from libswscale (surfaced by
	// astiav as the opaque "astiav: empty new context") is replaced
	// with a typed error naming the offending field. See
	// encoder_scaler_descriptors.go for the precondition list.
	srcPixFmt := input.Frame.PixelFormat()
	dstPixFmt := e.ScaledFrame.PixelFormat()
	if err := validateScalerDescriptors(inputResolution, srcPixFmt, *outputResolution, dstPixFmt); err != nil {
		// Errorf so the diagnostic survives non-Trace log levels.
		// Pinned-trap context: full source frame descriptors,
		// encoder codec context pixfmt, and whether a
		// hw_frames_ctx is attached on the encoder side. All four
		// are necessary to decide whether the bug is upstream
		// (camera/decoder produced a degenerate frame) or local
		// (encoder pixfmt did not get the late yuv420p injection in
		// time, leaving CodecContext.PixelFormat() at MEDIACODEC
		// and ScaledFrame.PixelFormat() ditto).
		hwFCAttached := e.Encoder.HardwareFramesContext(ctx) != nil
		logger.Errorf(ctx,
			"refusing to call sws_getContext with degenerate descriptors: %v "+
				"(input.Frame: w=%d h=%d pix_fmt=%s; encoder.CodecContext.PixelFormat=%s; encoder.HardwareFramesContext attached=%t)",
			err,
			input.Frame.Width(), input.Frame.Height(), srcPixFmt,
			e.CodecContext(ctx).PixelFormat(), hwFCAttached,
		)
		return fmt.Errorf("unable to create a scaler: %w", err)
	}

	s, err := scaler.NewSoftware(
		ctx,
		inputResolution, srcPixFmt,
		*outputResolution, dstPixFmt,
		astiav.SoftwareScaleContextFlagLanczos,
	)
	if err != nil {
		return fmt.Errorf("unable to create a scaler: %w", err)
	}

	e.Scaler = s
	return nil
}

func (e *Encoder[EF]) send(
	ctx context.Context,
	outPkt *astiav.Packet,
	pipelineSideData []any,
	outputStream *astiav.Stream,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	outPktWrapped := packet.BuildOutput(
		outPkt,
		packet.BuildStreamInfo(
			outputStream,
			e,
			pipelineSideData,
		),
	)

	if encoderDebug {
		if outPktWrapped.GetDuration() <= 0 {
			logger.Warnf(ctx, "packet duration is not set (pts:%d, dts:%d, time_base:%v, stream_tb:%v)",
				outPktWrapped.GetPTS(), outPktWrapped.GetDTS(),
				outPktWrapped.GetTimeBase(), outputStream.TimeBase())
		}
	}

	logger.Tracef(ctx, "encode-emit %s pts=%d key=%t",
		outPktWrapped.GetCodecParameters().MediaType(),
		outPktWrapped.GetPTS(),
		outPktWrapped.IsKey())
	logger.Tracef(ctx, "sending out %s: dts:%d; pts:%d", outPktWrapped.GetCodecParameters().MediaType(), outPktWrapped.GetDTS(), outPktWrapped.GetPTS())
	defer func() {
		logger.Tracef(ctx, "/send: %v %v", outPktWrapped.GetCodecParameters().MediaType(), _err)
	}()

	var err error
	select {
	case <-e.ClosureSignaler.CloseChan():
		err = io.ErrClosedPipe
	case <-ctx.Done():
		err = ctx.Err()
	case outputCh <- packetorframe.OutputUnion{Packet: &outPktWrapped}:
	}
	return err
}

func (e *Encoder[EF]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	e.Locker.Do(ctx, func() {
		callback(e.outputFormatContext)
	})
}

func (e *Encoder[EF]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	logger.Tracef(ctx, "NotifyAboutPacketSource: %s", source)
	defer func() { logger.Tracef(ctx, "/NotifyAboutPacketSource: %s", source) }()
	var errs []error
	ctx = belt.WithField(ctx, "encoder", e)
	ctx = belt.WithField(ctx, "source", source)
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		e.Locker.Do(ctx, func() {
			changed := false
			for _, inputStream := range fmtCtx.Streams() {
				if e.encoders[inputStream.Index()] != nil {
					continue
				}
				changed = true
				err := e.initEncoderAndOutputFor(
					ctx,
					inputStream.Index(),
					inputStream.CodecParameters(),
					inputStream.TimeBase(),
					nil,
				)
				switch {
				case err == nil:
				case errors.As(err, &codec.ErrNotDummy{}):
				default:
					errs = append(errs, fmt.Errorf("unable to initialize an output stream for input stream %d from source %s: %w", inputStream.Index(), source, err))
				}
			}

			if encoderWriteHeaderOnNotifyPacketSources && changed {
				logger.Debugf(ctx, "writing the header")
				err := e.outputFormatContext.WriteHeader(nil)
				if err == nil {
					e.headerIsWritten = true
				} else {
					errs = append(errs, fmt.Errorf("unable to write header: %w", err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (e *Encoder[EF]) ResetSoft(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetSoft")
	defer func() { logger.Debugf(ctx, "/ResetSoft: %v", _err) }()
	return xsync.DoA1R1(ctx, &e.Locker, e.resetSoft, ctx)
}

func (e *Encoder[EF]) resetSoft(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "resetSoft")
	defer func() { logger.Tracef(ctx, "/resetSoft: %v", _err) }()

	var errs []error
	for streamIndex, encoder := range e.encoders {
		if err := encoder.Encoder.Flush(ctx, nil); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset the encoder for stream #%d: %w", streamIndex, err))
		}
	}

	return errors.Join(errs...)
}

func (e *Encoder[EF]) ResetHard(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetHard")
	defer func() { logger.Debugf(ctx, "/ResetHard: %v", _err) }()
	return xsync.DoA1R1(ctx, &e.Locker, e.resetHard, ctx)
}

func (e *Encoder[EF]) resetHard(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "resetHard ts_ms=%d", logger.NowMS())
	defer func() { logger.Tracef(ctx, "/resetHard ts_ms=%d: %v", logger.NowMS(), _err) }()

	var errs []error
	for streamIndex, encoder := range e.encoders {
		streamIndex, encoder := streamIndex, encoder
		// Inside the LockDo callback the codec.Encoder argument is the
		// LOCKED variant (*codec.EncoderFullLocked). Closing through it
		// bypasses the outer EncoderFull.Close -> withLocked re-entry that
		// would deadlock on codec.EncoderFull.locker (already held by
		// LockDo). See streamEncoder.closeLocked.
		if err := encoder.Encoder.LockDo(ctx, func(ctx context.Context, locked codec.Encoder) error {
			if err := encoder.closeLocked(ctx, locked); err != nil {
				errs = append(errs, fmt.Errorf("unable to close the encoder for stream #%d: %w", streamIndex, err))
			}
			delete(e.encoders, streamIndex)
			delete(e.outputStreams, streamIndex)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("unable to lock-do encoder for stream #%d: %w", streamIndex, err))
		}
	}

	if err := e.EncoderFactory.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the encoder factory: %w", err))
	}

	return errors.Join(errs...)
}

func (e *Encoder[EF]) IsDirty(ctx context.Context) (_ret bool) {
	if e.isDirtyCache.Load() {
		return true
	}
	return xsync.DoA1R1(ctx, &e.Locker, e.isDirty, ctx)
}

func (e *Encoder[EF]) isDirty(ctx context.Context) (_ret bool) {
	logger.Tracef(ctx, "isDirty")
	defer func() { logger.Tracef(ctx, "/isDirty: %v", _ret) }()
	defer func() { e.isDirtyCache.Store(_ret) }()
	for _, encoder := range e.encoders {
		if encoder.Encoder.IsDirty() {
			return true
		}
	}
	return false
}

var _ Flusher = (*Encoder[codec.EncoderFactory])(nil)

func (e *Encoder[EF]) Flush(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Flush")
	defer func() { logger.Debugf(ctx, "/Flush: %v", _err) }()

	defer func() {
		if _err == nil {
			e.isDirtyCache.Store(false)
		}
	}()

	// Buffer must fit one error per encoder goroutine to avoid deadlock:
	// if a sender blocks, its wg.Done() never runs, preventing wg.Wait()
	// and close(errCh), which blocks the range loop.
	errCh := make(chan error, len(e.encoders))

	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		e.Locker.Do(ctx, func() {
			for streamIndex, encoder := range e.encoders {
				wg.Add(1)
				streamIndex, encoder := streamIndex, encoder
				ctx := belt.WithField(ctx, "stream_index", streamIndex)
				ctx = belt.WithField(ctx, "encoder", encoder)
				observability.Go(ctx, func(ctx context.Context) {
					defer wg.Done()
					err := e.drain(
						ctx,
						outputCh,
						encoder.Encoder.Flush,
						e.outputStreams[streamIndex],
						FrameInfo{
							StreamIndex: streamIndex,
						},
						encoder,
					)
					if err != nil {
						errCh <- fmt.Errorf("unable to flush the encoder for stream #%d: %w", streamIndex, err)
					}
				})
			}
		})
	})
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if errs != nil {
		return errors.Join(errs...)
	}
	return nil
}

func fixCodecParameters(
	ctx context.Context,
	params *astiav.CodecParameters,
) *astiav.CodecParameters {
	if params == nil {
		return nil
	}

	cp := astiav.AllocCodecParameters()
	setFinalizerFree(ctx, cp)
	params.Copy(cp)
	cp.SetCodecID(astiav.CodecIDNone)
	return cp
}
