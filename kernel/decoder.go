package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

const (
	decoderDebug = true
)

type Decoder[DF codec.DecoderFactory] struct {
	*closuresignaler.ClosureSignaler

	DecoderFactory        DF
	Locker                xsync.Mutex
	Decoders              map[int]*codec.Decoder
	OutputCodecParameters map[int]*astiav.CodecParameters

	FormatContext *astiav.FormatContext
	IsDirtyValue  atomic.Bool
}

var _ Abstract = (*Decoder[codec.DecoderFactory])(nil)
var _ packet.Sink = (*Decoder[codec.DecoderFactory])(nil)

func NewDecoder[DF codec.DecoderFactory](
	ctx context.Context,
	decoderFactory DF,
) *Decoder[DF] {
	d := &Decoder[DF]{
		ClosureSignaler:       closuresignaler.New(),
		DecoderFactory:        decoderFactory,
		Decoders:              map[int]*codec.Decoder{},
		FormatContext:         astiav.AllocFormatContext(),
		OutputCodecParameters: map[int]*astiav.CodecParameters{},
	}
	setFinalizerFree(ctx, d.FormatContext)
	return d
}

func (d *Decoder[DF]) Close(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &d.Locker, d.close, ctx)
}

func (d *Decoder[DF]) close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "close()")
	defer func() { logger.Debugf(ctx, "/close(): %v", _err) }()
	d.ClosureSignaler.Close(ctx)
	for key, decoder := range d.Decoders {
		err := decoder.Close(ctx)
		logger.Tracef(ctx, "decoder for stream #%d closed: %v", key, err)
		delete(d.Decoders, key)
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
	return xsync.DoA2R2(ctx, &d.Locker, d.getStreamDecoder, ctx, stream)
}

func (d *Decoder[DF]) getStreamDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (*codec.Decoder, error) {
	decoder := d.Decoders[stream.Index()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder != nil {
		return decoder, nil
	}
	decoder, err := d.DecoderFactory.NewDecoder(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize a decoder for stream %d: %w", stream.Index(), err)
	}
	assert(ctx, decoder != nil)
	logger.Tracef(ctx, "initialized a decoder: %s", decoder)
	d.Decoders[stream.Index()] = decoder
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

	return xsync.DoA3R1(ctx, &d.Locker, d.sendInputPacket, ctx, input, outputFramesCh)
}

func (d *Decoder[DF]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	decoder, err := d.getStreamDecoder(ctx, input.Stream)
	if err != nil {
		return fmt.Errorf("unable to get a stream decoder: %w", err)
	}
	ctx = belt.WithField(ctx, "decoder", decoder)

	if decoderDebug {
		logger.Tracef(ctx, "input packet: dur:%d; res:%s", input.Duration(), input.GetResolution())
	}

	if !encoderForceCopyTime {
		input.Packet.RescaleTs(input.Stream.TimeBase(), decoder.TimeBase())
	}

	if err := decoder.SendPacket(ctx, input.Packet); err != nil {
		logger.Debugf(ctx, "decoder.CodecContext().SendPacket(): %v", err)
		if errors.Is(err, astiav.ErrEagain) {
			return nil
		}
		return fmt.Errorf("unable to decode the packet: %w", err)
	}

	err = d.drain(ctx, outputFramesCh, decoder.Drain, input, input.Stream)
	if err != nil {
		return fmt.Errorf("unable to drain the decoder: %w", err)
	}

	return nil
}

func (d *Decoder[DF]) drain(
	ctx context.Context,
	outputFramesCh chan<- frame.Output,
	decoderDrainFn func(context.Context, codec.CallbackFrameReceiver) error,
	input packet.Input,
	outputStream *astiav.Stream,
) (_err error) {
	logger.Tracef(ctx, "drain")
	defer func() { logger.Tracef(ctx, "/drain: %v", _err) }()
	return decoderDrainFn(ctx, func(
		ctx context.Context,
		decoder *codec.DecoderLocked,
		caps astiav.CodecCapabilities,
		f *astiav.Frame,
	) error {
		logger.Tracef(ctx, "decoder.ReceiveFrame(): received a frame")

		timeBase := input.Stream.TimeBase()
		if timeBase.Num() == 0 {
			return fmt.Errorf("internal error: TimeBase is not set")
		}
		f.SetPictureType(astiav.PictureTypeNone)
		if f.Pts() == astiav.NoPtsValue {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame PTS from packet PTS: %d", input.Packet.Pts())
			}
			f.SetPts(input.Packet.Pts())
		}
		if f.Duration() <= 0 {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame duration from packet duration: %d", input.Packet.Duration())
			}
			f.SetDuration(input.Packet.Duration())
		}
		frameToSend := frame.BuildOutput(
			f,
			input.Packet.Pos(),
			frame.BuildStreamInfo(
				d.asSource(decoder.AsUnlocked()),
				d.getOutputCodecParameters(ctx, input.StreamIndex(), decoder),
				input.StreamIndex(), sourceNbStreams(ctx, input.Source),
				input.Stream.Duration(),
				input.Stream.AvgFrameRate(),
				timeBase,
				input.Packet.Duration(),
				input.PipelineSideData,
			),
		)
		var err error
		d.Locker.UDo(ctx, func() {
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case outputFramesCh <- frameToSend:
			}
		})
		return err
	})
}

type decoderAsSource[DF codec.DecoderFactory] struct {
	Kernel  *Decoder[DF]
	Decoder *codec.Decoder
}

var _ frame.Source = (*decoderAsSource[codec.DecoderFactory])(nil)
var _ codec.GetDecoderer = (*decoderAsSource[codec.DecoderFactory])(nil)

func (d *decoderAsSource[DF]) String() string {
	return d.Decoder.String()
}

func (d *decoderAsSource[DF]) GetDecoder() *codec.Decoder {
	return d.Decoder
}

func (d *Decoder[DF]) asSource(decoder *codec.Decoder) frame.Source {
	return &decoderAsSource[DF]{
		Kernel:  d,
		Decoder: decoder,
	}
}

func (d *Decoder[DF]) getOutputCodecParameters(
	ctx context.Context,
	streamIndex int,
	decoder *codec.DecoderLocked,
) *astiav.CodecParameters {
	if v, ok := d.OutputCodecParameters[streamIndex]; ok {
		return v
	}

	codecParams := astiav.AllocCodecParameters()
	setFinalizerFree(ctx, codecParams)
	decoder.ToCodecParameters(codecParams)
	switch codecParams.MediaType() {
	case astiav.MediaTypeVideo:
		codecParams.SetCodecID(astiav.CodecIDRawvideo)
	case astiav.MediaTypeAudio:
		// TODO: figure out which PCM is used here and set it
	}
	d.OutputCodecParameters[streamIndex] = codecParams
	return codecParams
}

func (d *Decoder[DF]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
}

func (d *Decoder[DF]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	d.Locker.Do(ctx, func() {
		callback(d.FormatContext)
	})
}

func (d *Decoder[DF]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	var errs []error
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		d.Locker.Do(ctx, func() {
			for _, inputStream := range fmtCtx.Streams() {
				_, err := d.getStreamDecoder(ctx, inputStream)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to get a stream decoder: %w", err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (d *Decoder[DF]) Reset(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &d.Locker, d.reset, ctx)
}

func (d *Decoder[DF]) reset(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()

	var errs []error
	for streamIndex, decoder := range d.Decoders {
		if err := decoder.Reset(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset the decoder for stream #%d: %w", streamIndex, err))
		}
	}

	return errors.Join(errs...)
}

func (d *Decoder[DF]) IsDirty(
	ctx context.Context,
) bool {
	if d.IsDirtyValue.Load() {
		return true
	}
	return xsync.DoA1R1(ctx, &d.Locker, d.isDirtyLocked, ctx)
}

func (d *Decoder[DF]) isDirtyLocked(
	ctx context.Context,
) (_ret bool) {
	logger.Debugf(ctx, "isDirty")
	defer func() { logger.Debugf(ctx, "/isDirty: %v", _ret) }()
	defer func() { d.IsDirtyValue.Store(_ret) }()
	for _, decoder := range d.Decoders {
		if decoder.IsDirty(ctx) {
			return true
		}
	}
	return false
}

func (d *Decoder[DF]) Flush(
	ctx context.Context,
	outputFrameCh chan<- frame.Output,
) (_err error) {
	logger.Debugf(ctx, "flush()")
	defer func() { logger.Debugf(ctx, "/flush(): %v", _err) }()
	if d.IsClosed() {
		return io.ErrClosedPipe
	}

	return xsync.DoA2R1(ctx, &d.Locker, d.flush, ctx, outputFrameCh)
}

func (d *Decoder[DF]) flush(
	ctx context.Context,
	outputFrameCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "flush()")
	defer func() { logger.Tracef(ctx, "/flush(): %v", _err) }()

	var errs []error
	for streamIndex, decoder := range d.Decoders {
		err := decoder.LockDo(ctx, func(ctx context.Context, decoder *codec.DecoderLocked) error {
			return d.drain(
				ctx,
				outputFrameCh,
				decoder.Flush,
				packet.Input{},
				d.FormatContext.Streams()[streamIndex],
			)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to drain the decoder for stream #%d: %w", streamIndex, err))
		}
	}
	return errors.Join(errs...)
}
