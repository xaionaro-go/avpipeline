package kernel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/resampler"
	"github.com/xaionaro-go/avpipeline/scaler"
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
)

type Encoder[EF codec.EncoderFactory] struct {
	*closuresignaler.ClosureSignaler

	EncoderFactory EF
	Locker         xsync.Mutex
	PTSDurDiff     *time.Duration

	encoders                  map[int]*streamEncoder
	outputStreams             map[int]*astiav.Stream
	StreamConfigurer          StreamConfigurer
	outputFormatContextLocker xsync.RWMutex
	outputFormatContext       *astiav.FormatContext
	headerIsWritten           bool
	isDirtyCache              atomic.Bool
}

var _ Abstract = (*Encoder[codec.EncoderFactory])(nil)
var _ packet.Source = (*Encoder[codec.EncoderFactory])(nil)

// TODO: remove this: encoder should calculate timestamps from scratch, instead of reusing these
type frameInfo struct {
	PTS         int64
	DTS         int64
	TimeBase    astiav.Rational
	Duration    int64
	StreamIndex int
}

func (p *frameInfo) Bytes() []byte {
	b := make([]byte, 48)
	binary.NativeEndian.PutUint64(b[0:8], uint64(p.PTS))
	binary.NativeEndian.PutUint64(b[8:16], uint64(p.DTS))
	binary.NativeEndian.PutUint64(b[16:24], uint64(p.TimeBase.Num()))
	binary.NativeEndian.PutUint64(b[24:32], uint64(p.TimeBase.Den()))
	binary.NativeEndian.PutUint64(b[32:40], uint64(p.Duration))
	binary.NativeEndian.PutUint64(b[40:48], uint64(p.StreamIndex))
	return b
}

func frameInfoFromBytes(b []byte) *frameInfo {
	if len(b) < 48 {
		return nil
	}
	num := int(binary.NativeEndian.Uint64(b[16:24]))
	den := int(binary.NativeEndian.Uint64(b[24:32]))
	return &frameInfo{
		PTS:         int64(binary.NativeEndian.Uint64(b[0:8])),
		DTS:         int64(binary.NativeEndian.Uint64(b[8:16])),
		TimeBase:    astiav.NewRational(num, den),
		Duration:    int64(binary.NativeEndian.Uint64(b[32:40])),
		StreamIndex: int(binary.NativeEndian.Uint64(b[40:48])),
	}
}

type streamEncoder struct {
	codec.Encoder
	Resampler       *resampler.Resampler
	ResampledFrames []*astiav.Frame
	Scaler          scaler.Scaler
	ScaledFrame     *astiav.Frame
	LastInitTS      time.Time
}

func NewEncoder[EF codec.EncoderFactory](
	ctx context.Context,
	encoderFactory EF,
	streamConfigurer StreamConfigurer,
) *Encoder[EF] {
	logger.Tracef(ctx, "NewEncoder")
	defer func() { logger.Tracef(ctx, "/NewEncoder") }()
	e := &Encoder[EF]{
		ClosureSignaler:     closuresignaler.New(),
		EncoderFactory:      encoderFactory,
		encoders:            map[int]*streamEncoder{},
		StreamConfigurer:    streamConfigurer,
		outputFormatContext: astiav.AllocFormatContext(),
		outputStreams:       make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, e.outputFormatContext)
	return e
}

func (e *Encoder[EF]) Close(ctx context.Context) error {
	e.ClosureSignaler.Close(ctx)
	for key, encoder := range e.encoders {
		err := encoder.Close(ctx)
		logger.Debugf(ctx, "encoder closed: %v", err)
		delete(e.encoders, key)
	}
	return nil
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
		outputStream = e.outputFormatContext.NewStream(encoder.Codec())
	})
	if outputStream == nil {
		return fmt.Errorf("unable to initialize an output stream")
	}
	if err := encoder.ToCodecParameters(outputStream.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy codec parameters from the encoder to the output stream: %w", err)
	}

	err := e.configureOutputStream(ctx, outputStream, streamIndex, encoder.CodecContext().TimeBase())
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
		"resulting output stream for input stream %d: %d: %s: %s: %s: %s: %s",
		streamIndex,
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
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
	res := codec.Resolution{
		Width:  uint32(params.Width()),
		Height: uint32(params.Height()),
	}
	codecID := params.CodecID()
	pixFmt := params.PixelFormat()
	logger.Debugf(ctx, "initEncoderAndOutputFor(%d): %s, %v/%s", streamIndex, codecID, res, pixFmt)
	defer func() {
		logger.Debugf(ctx, "/initEncoderAndOutputFor(%d): %s, %v/%s: %v", streamIndex, codecID, res, pixFmt, _err)
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
		return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", streamIndex, err)
	}

	encoder := e.encoders[streamIndex]
	if encoder == nil {
		return fmt.Errorf("internal error: encoder for stream index %d is nil after an explicit request for initialization", streamIndex)
	}
	switch {
	case codec.IsEncoderCopy(encoder.Encoder):
		err = e.initOutputStreamCopy(ctx, streamIndex, params, timeBase)
	case codec.IsEncoderRaw(encoder.Encoder):
		return nil
	default:
		err = e.initOutputStream(ctx, streamIndex, encoder)
	}
	if err != nil {
		return fmt.Errorf("unable to init an output stream for encoder %s for input stream #%d: %w", encoder, streamIndex, err)
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
	logger.Debugf(ctx, "initEncoderFor(ctx, stream[%d])", streamIndex)
	defer func() { logger.Debugf(ctx, "/initEncoderFor(ctx, stream[%d]): %v", streamIndex, _err) }()

	if timeBase.Num() == 0 {
		return fmt.Errorf("TimeBase must be set")
	}

	var opts []codec.EncoderFactoryOption
	if getDecoderer, ok := frameSource.(codec.GetDecoderer); ok {
		opts = append(opts, codec.EncoderFactoryOptionGetDecoderer{GetDecoderer: getDecoderer})
	} else {
		if frameSource != nil {
			logger.Debugf(ctx, "the frame source does not implement codec.GetDecoderer: %T", frameSource)
		}
		opts = append(opts, codec.EncoderFactoryOptionOnlyDummy{OnlyDummy: true})
	}

	encoderInstance, err := e.EncoderFactory.NewEncoder(ctx, params, timeBase, opts...)
	if err != nil {
		return fmt.Errorf("cannot initialize an encoder for stream %d: %w", streamIndex, err)
	}
	if !codec.IsDummyEncoder(encoderInstance) && encoderInstance.CodecContext() == nil {
		return fmt.Errorf("the encoder factory produced an encoder %T with nil CodecContext", encoderInstance)
	}

	encoder := &streamEncoder{Encoder: encoderInstance}
	e.encoders[streamIndex] = encoder
	return nil
}

func (e *Encoder[EF]) String() string {
	return fmt.Sprintf("Encoder(%s)", e.EncoderFactory)
}

func (e *Encoder[EF]) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

type ErrNotCopyEncoder struct{}

func (ErrNotCopyEncoder) Error() string {
	return "one cannot send undecoded packets via an encoder; it is required to decode them first and send as raw frames"
}

func (e *Encoder[EF]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	ctx = belt.WithField(ctx, "encoder", e)
	logger.Tracef(ctx, "SendInputPacket: %s", input.GetMediaType())
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %s: %v", input.GetMediaType(), _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}
	ctx = belt.WithField(ctx, "mode", "packet")
	streamEncoder, err := xsync.DoR2(xsync.WithNoLogging(ctx, true), &e.Locker, func() (*streamEncoder, error) {
		streamEncoder := e.encoders[input.GetStreamIndex()]
		logger.Tracef(ctx, "e.Encoders[%d] == %v", input.GetStreamIndex(), streamEncoder)
		if streamEncoder == nil {
			input.Stream.AvgFrameRate()
			logger.Debugf(ctx, "an encoder is not initialized, yet")
			err := e.initEncoderAndOutputFor(
				ctx,
				input.GetStreamIndex(),
				input.CodecParameters(),
				input.GetStream().TimeBase(),
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
	err = e.send(ctx, pkt, input.PipelineSideData, outputStream, outputPacketsCh)
	if err != nil {
		return fmt.Errorf("unable to send a packet: %w", err)
	}
	return nil
}

func (e *Encoder[EF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputFrame")
	defer func() { logger.Tracef(ctx, "/SendInputFrame: %v", _err) }()
	if e.IsClosed() {
		return io.ErrClosedPipe
	}

	ctx = belt.WithField(ctx, "encoder", e)
	ctx = belt.WithField(ctx, "mode", "frame")
	ctx = belt.WithField(ctx, "media_type", input.GetMediaType())
	ctx = belt.WithField(ctx, "stream_index", input.GetStreamIndex())
	ctx = xsync.WithNoLogging(ctx, true)

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
		case outputFramesCh <- frame.BuildOutput(
			frame.CloneAsReferenced(input.Frame),
			input.StreamInfo,
		):
		}
		return err
	}

	outputStream := xsync.DoR1(ctx, &e.Locker, func() *astiav.Stream {
		outputStream := e.outputStreams[input.GetStreamIndex()]
		assert(ctx, outputStream != nil, "outputStream != nil")
		return outputStream
	})

	if enableStreamCodecParametersUpdates {
		streamEncoder.Encoder.LockDo(ctx, func(ctx context.Context, encoder codec.Encoder) error {
			getInitTSer, ok := streamEncoder.Encoder.(interface{ GetInitTS() time.Time })
			if !ok {
				return nil
			}
			initTS := getInitTSer.GetInitTS()
			if streamEncoder.LastInitTS.Before(initTS) {
				logger.Debugf(ctx, "updating the codec parameters")
				streamEncoder.Encoder.ToCodecParameters(outputStream.CodecParameters())
				streamEncoder.LastInitTS = initTS
			}
			return nil
		})
	}

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
		encoderMediaType := streamEncoder.Encoder.MediaType()
		assert(ctx, inputMediaType == encoderMediaType, inputMediaType, encoderMediaType)
		assert(ctx, outputMediaType == encoderMediaType, outputMediaType, encoderMediaType)

		if isEmptyFrame(ctx, input) {
			logger.Errorf(ctx, "the input frame is empty; dropping it")
			return nil
		}
	}

	fittedFrames, err := streamEncoder.fitFrameForEncoding(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to fit the frame for encoding: %w", err)
	}

	if len(fittedFrames) == 0 {
		logger.Tracef(ctx, "the frame was dropped by the fitter")
		return nil
	}

	frameInfo := frameInfo{
		PTS:         input.Pts(),
		DTS:         input.PktDts(),
		Duration:    input.Frame.Duration(),
		StreamIndex: input.StreamIndex,
	}
	return streamEncoder.Encoder.LockDo(ctx, func(ctx context.Context, encoder codec.Encoder) error {
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
			if frameInfo.TimeBase.Num() == 0 {
				frameInfo.TimeBase = input.GetTimeBase()
			}

			fittedFrame.SetOpaque(frameInfo.Bytes())
			err := encoder.SendFrame(ctx, fittedFrame)
			if err != nil {
				return fmt.Errorf("unable to send a %s frame to the encoder: %w", outputMediaType, err)
			}
		}

		err = e.drain(ctx, outputPacketsCh, encoder.Drain, outputStream, frameInfo)
		if err != nil {
			return fmt.Errorf("unable to drain: %w", err)
		}

		return nil
	})
}

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
	outputPacketsCh chan<- packet.Output,
	encoderDrainFn func(context.Context, codec.CallbackPacketReceiver) error,
	outputStream *astiav.Stream,
	frameInfo frameInfo,
) (_err error) {
	logger.Tracef(ctx, "drain")
	defer func() { logger.Tracef(ctx, "/drain: %v", _err) }()

	packetCount := 0
	return encoderDrainFn(ctx, func(
		ctx context.Context,
		encoder *codec.EncoderFullLocked,
		caps astiav.CodecCapabilities,
		pkt *astiav.Packet,
	) error {
		packetCount++
		opaque := pkt.Opaque()
		logger.Tracef(ctx, "encoder.ReceivePacket(): got the %dth %s packet, resulting size: %d (pts: %d); opaque size: %d", packetCount, outputStream.CodecParameters().MediaType(), pkt.Size(), pkt.Pts(), len(opaque))

		pkt.SetStreamIndex(outputStream.Index())

		if opaque != nil {
			frameInfoPtr := frameInfoFromBytes(opaque)
			if frameInfoPtr == nil {
				return fmt.Errorf("unable to parse the frame info from the packet opaque data: %v", opaque)
			}
			frameInfo = *frameInfoPtr
		}

		if caps&astiav.CodecCapabilityDr1 != 0 || encoderForceCopyTime {
			logger.Tracef(ctx, "setting manually the packet timestamps")
			// get rid of this copying below. We should calculate these values
			// from scratch, instead of just copying them.
			if pkt.Duration() <= 0 {
				pkt.SetDuration(frameInfo.Duration)
			}
			pkt.SetDts(frameInfo.DTS)
			pkt.SetPts(frameInfo.PTS)
			pkt.RescaleTs(frameInfo.TimeBase, outputStream.TimeBase())
		}

		//pkt.SetPos(-1) // <- TODO: should this happen? why?
		if pkt.Dts() > pkt.Pts() && pkt.Dts() != consts.NoPTSValue && pkt.Pts() != consts.NoPTSValue {
			if encoderDTSHigherPTSCorrect {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) correcting DTS to %d", pkt.Dts(), pkt.Pts(), pkt.Pts())
				pkt.SetDts(pkt.Pts())
			} else {
				logger.Errorf(ctx, "DTS (%d) > PTS (%d) skipping the packet", pkt.Dts(), pkt.Pts())
				packet.Pool.Put(pkt)
				return nil
			}
		}

		err := e.send(ctx, pkt, nil, outputStream, outputPacketsCh)
		if err != nil {
			return fmt.Errorf("unable to send a packet: %w", err)
		}

		return nil
	})
}

func (e *streamEncoder) fitFrameForEncoding(
	ctx context.Context,
	input frame.Input,
) (fittedFrames []*astiav.Frame, _err error) {
	logger.Tracef(ctx, "fitFrameForEncoding: %s", e.MediaType())
	defer func() { logger.Tracef(ctx, "/fitFrameForEncoding: %s: %v %v", e.MediaType(), fittedFrames, _err) }()

	switch e.MediaType() {
	case astiav.MediaTypeVideo:
		res := e.GetResolution(ctx)
		if res == nil {
			return nil, fmt.Errorf("unable to get the resolution from the encoder")
		}
		if encoderDebug {
			logger.Tracef(ctx, "input frame: %dx%d (%s); encoder resolution: %s", input.Frame.Width(), input.Frame.Height(), input.CodecParameters.CodecID(), res)
		}
		// TODO: also check if non-hardware-backed pixel formats do match
		if input.Frame.Width() == int(res.Width) && input.Frame.Height() == int(res.Height) {
			return []*astiav.Frame{input.Frame}, nil
		}
		logger.Tracef(ctx, "scaling the frame from %dx%d/%s to %s/%s", input.Frame.Width(), input.Frame.Height(), input.PixelFormat(), res, e.Encoder.CodecContext().PixelFormat())
		scaledFrame, err := e.getScaledFrame(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("unable to scale the frame from %dx%d/%s to %s/%s: %w", input.Frame.Width(), input.Frame.Height(), input.PixelFormat(), res, e.Encoder.CodecContext().PixelFormat(), err)
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
		encPCMFmt := e.Encoder.GetPCMAudioFormat(ctx)
		if encPCMFmt == nil {
			return []*astiav.Frame{input.Frame}, nil
		}
		if encPCMFmt.Equal(*inPCMFmt) {
			return []*astiav.Frame{input.Frame}, nil
		}
		resampledFrames, err := e.getResampledFrames(ctx, input.Frame, *inPCMFmt, *encPCMFmt)
		if err != nil {
			return nil, fmt.Errorf("unable to resample the audio frame: %w", err)
		}
		return resampledFrames, nil
	default:
		logger.Debugf(ctx, "unsupported media type: %s", e.MediaType())
		return []*astiav.Frame{input.Frame}, nil
	}
}

func getPCMAudioFormatFromFrame(
	ctx context.Context,
	frame *astiav.Frame,
) *codec.PCMAudioFormat {
	if frame == nil {
		return nil
	}
	return &codec.PCMAudioFormat{
		SampleFormat:  frame.SampleFormat(),
		SampleRate:    frame.SampleRate(),
		ChannelLayout: frame.ChannelLayout(),
		ChunkSize:     frame.NbSamples(),
	}
}

func (e *streamEncoder) getResampledFrames(
	ctx context.Context,
	inputFrame *astiav.Frame,
	inPCMFmt codec.PCMAudioFormat,
	outPCMFmt codec.PCMAudioFormat,
) (resampledFrames []*astiav.Frame, _err error) {
	logger.Tracef(ctx, "getResampledFrames: in:%v; out:%v", inPCMFmt, outPCMFmt)
	defer func() {
		logger.Tracef(ctx, "/getResampledFrames: in:%v; out:%v: %v %v", inPCMFmt, outPCMFmt, resampledFrames, _err)
	}()

	err := e.prepareResampler(ctx, inPCMFmt, outPCMFmt)
	if err != nil {
		return nil, fmt.Errorf("unable to prepare the resampler: %w", err)
	}

	err = e.Resampler.SendFrame(ctx, inputFrame)
	if err != nil {
		return nil, fmt.Errorf("unable to send the frame: %w", err)
	}

	for idx := 0; ; idx++ {
		for idx >= len(e.ResampledFrames) {
			newFrame, err := e.Resampler.AllocateOutputFrame(ctx)
			if err != nil {
				return nil, fmt.Errorf("unable to allocate a new output frame: %w", err)
			}
			e.ResampledFrames = append(e.ResampledFrames, newFrame)
		}

		outputFrame := e.ResampledFrames[idx]
		err := e.Resampler.ReceiveFrame(ctx, outputFrame)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			logger.Tracef(ctx, "resampler.ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			if isEOF || isEAgain {
				break
			}
			return nil, fmt.Errorf("unable to receive the frame from the resampler: %w", err)
		}
		resampledFrames = append(resampledFrames, outputFrame)
	}

	return resampledFrames, nil
}

func (e *streamEncoder) prepareResampler(
	ctx context.Context,
	inPCMFmt codec.PCMAudioFormat,
	outPCMFmt codec.PCMAudioFormat,
) (_err error) {
	logger.Tracef(ctx, "prepareResampler: in:%v; out:%v", inPCMFmt, outPCMFmt)
	defer func() { logger.Tracef(ctx, "/prepareResampler: in:%v; out:%v: %v", inPCMFmt, outPCMFmt, _err) }()

	if e.Resampler != nil {
		if outPCMFmt.Equal(e.Resampler.FormatOutput) {
			logger.Tracef(ctx, "reusing the resampler")
			return nil
		}
		logger.Debugf(ctx, "reinitializing the resampler: %v->(%v->%v)", inPCMFmt, e.Resampler.FormatOutput, outPCMFmt)
		if err := e.Resampler.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close the resampler: %v", err)
		}
	}

	s, err := resampler.New(
		ctx,
		outPCMFmt,
	)
	if err != nil {
		return fmt.Errorf("unable to create a resampler: %w", err)
	}

	e.Resampler = s
	e.ResampledFrames = nil
	return nil
}

func (e *streamEncoder) getScaledFrame(
	ctx context.Context,
	input frame.Input,
) (_ret *astiav.Frame, _err error) {
	logger.Tracef(ctx, "getScaledFrame")
	defer func() { logger.Tracef(ctx, "/getScaledFrame: %v", _err) }()

	err := e.prepareScaler(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("unable to get a scaler: %w", err)
	}

	e.Scaler.ScaleFrame(ctx, input.Frame, e.ScaledFrame)
	return e.ScaledFrame, nil
}

func (e *streamEncoder) prepareScaler(
	ctx context.Context,
	input frame.Input,
) (_err error) {
	inputResolution := input.GetResolution()
	outputResolution := e.Encoder.GetResolution(ctx)
	logger.Tracef(ctx, "prepareScaler: %v/%v->%v/%v", inputResolution, input.PixelFormat(), outputResolution, e.Encoder.CodecContext().PixelFormat())
	defer func() {
		logger.Tracef(ctx, "/prepareScaler: %v/%v->%v/%v: %v", inputResolution, input.PixelFormat(), outputResolution, e.Encoder.CodecContext().PixelFormat(), _err)
	}()

	if outputResolution == nil {
		return fmt.Errorf("unable to get the resolution from the encoder")
	}

	if e.Scaler != nil {
		if e.Scaler.SourceResolution() == inputResolution && e.Scaler.DestinationResolution() == *outputResolution && input.PixelFormat() == e.Scaler.SourcePixelFormat() && e.Encoder.CodecContext().PixelFormat() == e.Scaler.DestinationPixelFormat() {
			logger.Tracef(ctx, "reusing the scaler")
			return nil
		}
		if err := e.Scaler.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close the scaler: %v", err)
		}
	}

	e.ScaledFrame = astiav.AllocFrame()
	setFinalizerFree(ctx, e.ScaledFrame)
	e.ScaledFrame.SetWidth(int(outputResolution.Width))
	e.ScaledFrame.SetHeight(int(outputResolution.Height))
	e.ScaledFrame.SetPixelFormat(e.Encoder.CodecContext().PixelFormat())
	if err := e.ScaledFrame.AllocBuffer(0); err != nil { // TODO(memleak): make sure the buffer is not already allocated, otherwise it may lead to a memleak
		return fmt.Errorf("unable to allocate a buffer for the scaled frame: %w", err)
	}

	s, err := scaler.NewSoftware(
		ctx,
		inputResolution, input.Frame.PixelFormat(),
		*outputResolution, e.ScaledFrame.PixelFormat(),
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
	out chan<- packet.Output,
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
		if outPktWrapped.Duration() <= 0 {
			logger.Warnf(ctx, "packet duration is not set")
		}
	}

	logger.Tracef(ctx, "sending out %s: dts:%d; pts:%d", outPktWrapped.CodecParameters().MediaType(), outPktWrapped.Dts(), outPktWrapped.Pts())
	defer func() { logger.Tracef(ctx, "/send: %v %v", outPktWrapped.CodecParameters().MediaType(), _err) }()

	var err error
	select {
	case <-e.ClosureSignaler.CloseChan():
		err = io.ErrClosedPipe
	case <-ctx.Done():
		err = ctx.Err()
	case out <- outPktWrapped:
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

func (e *Encoder[EF]) Reset(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &e.Locker, e.reset, ctx)
}

func (e *Encoder[EF]) reset(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()

	var errs []error
	for streamIndex, encoder := range e.encoders {
		if err := encoder.Flush(ctx, nil); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset the encoder for stream #%d: %w", streamIndex, err))
		}
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
		if encoder.IsDirty() {
			return true
		}
	}
	return false
}

var _ Flusher = (*Encoder[codec.EncoderFactory])(nil)

func (e *Encoder[EF]) Flush(
	ctx context.Context,
	outputPacketCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Debugf(ctx, "Flush")
	defer func() { logger.Debugf(ctx, "/Flush: %v", _err) }()

	defer func() {
		if _err == nil {
			e.isDirtyCache.Store(false)
		}
	}()

	errCh := make(chan error, 1)

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
						outputPacketCh,
						encoder.Flush,
						e.outputStreams[streamIndex],
						frameInfo{
							StreamIndex: streamIndex,
						},
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
