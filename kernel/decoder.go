package kernel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	DecoderMaxPacketInfoStorageSize = 1024
)

const (
	decoderDebug = true
)

// TODO: remove this: encoder should calculate timestamps from scratch, instead of reusing these
type packetInfo struct {
	PTS         int64
	DTS         int64
	Duration    int64
	StreamIndex int
}

func (p *packetInfo) Bytes() []byte {
	b := make([]byte, 40)
	binary.NativeEndian.PutUint64(b[0:8], uint64(p.PTS))
	binary.NativeEndian.PutUint64(b[8:16], uint64(p.DTS))
	binary.NativeEndian.PutUint64(b[24:32], uint64(p.Duration))
	binary.NativeEndian.PutUint64(b[32:40], uint64(p.StreamIndex))
	return b
}

func packetInfoFromBytes(b []byte) *packetInfo {
	if len(b) < 40 {
		return nil
	}
	return &packetInfo{
		PTS:         int64(binary.NativeEndian.Uint64(b[0:8])),
		DTS:         int64(binary.NativeEndian.Uint64(b[8:16])),
		Duration:    int64(binary.NativeEndian.Uint64(b[24:32])),
		StreamIndex: int(binary.NativeEndian.Uint64(b[32:40])),
	}
}

type Decoder[DF codec.DecoderFactory] struct {
	*closuresignaler.ClosureSignaler

	DecoderFactory        DF
	Locker                xsync.Mutex
	Decoders              map[int]*codec.Decoder
	OutputCodecParameters map[int]*astiav.CodecParameters
	StreamInfo            map[int]*frame.StreamInfo

	FormatContext *astiav.FormatContext
	IsDirtyCache  atomic.Bool
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
		StreamInfo:            map[int]*frame.StreamInfo{},
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
	streamDecoder, err := d.getStreamDecoder(ctx, input.Stream)
	if err != nil {
		return fmt.Errorf("unable to get a stream decoder: %w", err)
	}
	ctx = belt.WithField(ctx, "decoder", streamDecoder)

	if decoderDebug {
		logger.Tracef(ctx, "input packet: dur:%d; res:%s", input.Duration(), input.GetResolution())
	}

	timeBase := input.Stream.TimeBase()
	if timeBase.Num() == 0 {
		return fmt.Errorf("internal error: TimeBase is not set")
	}

	if !encoderForceCopyTime {
		input.Packet.RescaleTs(input.Stream.TimeBase(), streamDecoder.TimeBase())
	}

	return streamDecoder.LockDo(ctx, func(
		ctx context.Context,
		decoder *codec.DecoderLocked,
	) (_err error) {
		streamIndex := input.StreamIndex()

		streamInfo := d.StreamInfo[streamIndex]
		if streamInfo == nil {
			streamInfo = &frame.StreamInfo{
				Source:           d.asSource(decoder.AsUnlocked()),
				CodecParameters:  d.getOutputCodecParameters(ctx, streamIndex, decoder),
				StreamIndex:      streamIndex,
				StreamsCount:     sourceNbStreams(ctx, input.Source),
				TimeBase:         timeBase,
				Duration:         input.Packet.Duration(),
				PipelineSideData: nil,
			}
			d.StreamInfo[streamIndex] = streamInfo
		}

		packetInfo := packetInfo{
			PTS:         input.Packet.Pts(),
			DTS:         input.Packet.Dts(),
			Duration:    input.Packet.Duration(),
			StreamIndex: streamIndex,
		}

		input.Packet.SetOpaque(packetInfo.Bytes())

		if err := decoder.SendPacket(ctx, input.Packet); err != nil {
			logger.Debugf(ctx, "decoder.CodecContext().SendPacket(): %v", err)
			/*if errors.Is(err, astiav.ErrEagain) {
				return nil
			}*/
			return fmt.Errorf("unable to decode the packet: %w", err)
		}

		err = d.drain(ctx, outputFramesCh, decoder.Drain, packetInfo)
		if err != nil {
			return fmt.Errorf("unable to drain the decoder: %w", err)
		}

		return nil
	})
}

func (d *Decoder[DF]) drain(
	ctx context.Context,
	outputFramesCh chan<- frame.Output,
	decoderDrainFn func(context.Context, codec.CallbackFrameReceiver) error,
	packetInfo packetInfo,
) (_err error) {
	logger.Tracef(ctx, "drain")
	defer func() { logger.Tracef(ctx, "/drain: %v", _err) }()
	return decoderDrainFn(ctx, func(
		ctx context.Context,
		decoder *codec.DecoderLocked,
		caps astiav.CodecCapabilities,
		f *astiav.Frame,
	) error {
		ctx = belt.WithField(ctx, "decoder", decoder)
		opaque := f.Opaque()
		logger.Tracef(ctx, "decoder.ReceiveFrame(): received a frame (opaque len: %d)", len(opaque))

		f.SetPictureType(astiav.PictureTypeNone)

		if len(opaque) > 0 {
			packetInfoPtr := packetInfoFromBytes(f.Opaque())
			if packetInfoPtr == nil {
				return fmt.Errorf("unable to get packet info from frame opaque data: %v", f.Opaque())
			}
			packetInfo = *packetInfoPtr
		}
		logger.Tracef(ctx, "decoder.ReceiveFrame(): packetInfo: %+v", packetInfo)

		streamInfo := d.StreamInfo[packetInfo.StreamIndex]
		if streamInfo == nil {
			return fmt.Errorf("unable to find stream info for stream index %d", packetInfo.StreamIndex)
		}

		if f.Pts() == astiav.NoPtsValue {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame PTS from packet PTS: %d", packetInfo.PTS)
			}
			f.SetPts(packetInfo.PTS)
		}

		if f.Duration() <= 0 {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame duration from packet duration: %d", packetInfo.Duration)
			}
			f.SetDuration(packetInfo.Duration)
		}
		frameToSend := frame.BuildOutput(
			f,
			streamInfo,
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
	if d.IsDirtyCache.Load() {
		return true
	}
	return xsync.DoA1R1(ctx, &d.Locker, d.isDirtyLocked, ctx)
}

func (d *Decoder[DF]) isDirtyLocked(
	ctx context.Context,
) (_ret bool) {
	logger.Debugf(ctx, "isDirty")
	defer func() { logger.Debugf(ctx, "/isDirty: %v", _ret) }()
	defer func() { d.IsDirtyCache.Store(_ret) }()
	for _, decoder := range d.Decoders {
		if decoder.IsDirty(ctx) {
			return true
		}
	}
	return false
}

var _ Flusher = (*Decoder[codec.DecoderFactory])(nil)

func (d *Decoder[DF]) Flush(
	ctx context.Context,
	_ chan<- packet.Output,
	outputFrameCh chan<- frame.Output,
) (_err error) {
	logger.Debugf(ctx, "Flush()")
	defer func() { logger.Debugf(ctx, "/Flush(): %v", _err) }()

	if d.IsClosed() {
		return io.ErrClosedPipe
	}

	defer func() {
		if _err == nil {
			d.IsDirtyCache.Store(false)
		}
	}()

	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		d.Locker.Do(ctx, func() {
			for streamIndex, decoder := range d.Decoders {
				wg.Add(1)
				streamIndex, decoder := streamIndex, decoder
				ctx := belt.WithField(ctx, "stream_index", streamIndex)
				ctx = belt.WithField(ctx, "decoder", decoder)
				observability.Go(ctx, func(ctx context.Context) {
					defer wg.Done()
					err := decoder.LockDo(ctx, func(ctx context.Context, decoder *codec.DecoderLocked) error {
						return xsync.DoR1(ctx, &d.Locker, func() error {
							return d.drain(
								ctx,
								outputFrameCh,
								decoder.Flush,
								packetInfo{
									StreamIndex: streamIndex,
								},
							)
						})
					})
					if err != nil {
						errCh <- fmt.Errorf("unable to drain the decoder for stream #%d: %w", streamIndex, err)
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
