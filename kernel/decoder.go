// decoder.go implements the Decoder kernel for decoding media packets into frames.

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
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	DecoderMaxPacketInfoStorageSize = 1024
)

const (
	decoderDebug           = true
	decoderSendBlankFrames = true
	enableAntiStucking     = false
)

type packetInfoFlags uint64

const (
	packetInfoFlagFlush packetInfoFlags = 1 << iota
)

// TODO: delete this function; just save the PipelineSideData in Decoder, and store only a packet ID
// in packetInfo to recover all the actual info about the packet from Decoder.
func packetInfoFlagsFromPipelineSideData(sideData globaltypes.PipelineSideData) packetInfoFlags {
	var flags packetInfoFlags
	if sideData.Contains(SideFlagFlush{}) {
		flags |= packetInfoFlagFlush
	}
	return flags
}

func (p packetInfoFlags) PipelineSideData() globaltypes.PipelineSideData {
	var sideData globaltypes.PipelineSideData
	if p&packetInfoFlagFlush != 0 {
		sideData = append(sideData, SideFlagFlush{})
	}
	return sideData
}

// TODO: remove this: encoder should calculate timestamps from scratch, instead of reusing these
type packetInfo struct {
	PTS         int64
	DTS         int64
	Duration    int64
	StreamIndex int
	Flags       packetInfoFlags
}

func (p *packetInfo) Bytes() []byte {
	b := make([]byte, 48)
	binary.NativeEndian.PutUint64(b[0:8], uint64(p.PTS))
	binary.NativeEndian.PutUint64(b[8:16], uint64(p.DTS))
	binary.NativeEndian.PutUint64(b[24:32], uint64(p.Duration))
	binary.NativeEndian.PutUint64(b[32:40], uint64(p.StreamIndex))
	binary.NativeEndian.PutUint64(b[40:48], uint64(p.Flags))
	return b
}

func packetInfoFromBytes(b []byte) *packetInfo {
	if len(b) < 48 {
		return nil
	}
	return &packetInfo{
		PTS:         int64(binary.NativeEndian.Uint64(b[0:8])),
		DTS:         int64(binary.NativeEndian.Uint64(b[8:16])),
		Duration:    int64(binary.NativeEndian.Uint64(b[24:32])),
		StreamIndex: int(binary.NativeEndian.Uint64(b[32:40])),
		Flags:       packetInfoFlags(binary.NativeEndian.Uint64(b[40:48])),
	}
}

type StreamDecoder struct {
	*codec.Decoder
	SentBlankKeyFrame bool
}

type Decoder[DF codec.DecoderFactory] struct {
	*closuresignaler.ClosureSignaler

	DecoderFactory        DF
	Locker                xsync.Mutex
	Decoders              map[int]*StreamDecoder
	OutputCodecParameters map[int]*astiav.CodecParameters
	StreamInfo            xsync.Map[int, *frame.StreamInfo]

	FormatContext    *astiav.FormatContext
	IsDirtyCache     atomic.Bool
	AllowBlankFrames *atomic.Bool

	SentPacketsWithoutDecodingFrames uint64
}

var (
	_ Abstract    = (*Decoder[codec.DecoderFactory])(nil)
	_ packet.Sink = (*Decoder[codec.DecoderFactory])(nil)
)

func NewDecoder[DF codec.DecoderFactory](
	ctx context.Context,
	decoderFactory DF,
) *Decoder[DF] {
	d := &Decoder[DF]{
		ClosureSignaler:       closuresignaler.New(),
		DecoderFactory:        decoderFactory,
		Decoders:              map[int]*StreamDecoder{},
		FormatContext:         astiav.AllocFormatContext(),
		OutputCodecParameters: map[int]*astiav.CodecParameters{},
		AllowBlankFrames:      &atomic.Bool{},
	}
	setFinalizerFree(ctx, d.FormatContext)
	return d
}

func (d *Decoder[DF]) Close(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &d.Locker, d.closeLocked, ctx)
}

func (d *Decoder[DF]) closeLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "closeLocked()")
	defer func() { logger.Debugf(ctx, "/closeLocked(): %v", _err) }()
	d.ClosureSignaler.Close(ctx)

	var errs []error
	if err := d.DecoderFactory.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset the decoder factory: %w", err))
	}
	for key, decoder := range d.Decoders {
		if err := decoder.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the decoder for stream #%d: %w", key, err))
		}
		delete(d.Decoders, key)
	}
	if err := d.FormatContext.Flush(); err != nil {
		errs = append(errs, fmt.Errorf("unable to flush the format context: %w", err))
	}
	return errors.Join(errs...)
}

func (d *Decoder[DF]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(d)
}

func (d *Decoder[DF]) String() string {
	return fmt.Sprintf("Decoder(%s)", d.DecoderFactory)
}

func (d *Decoder[DF]) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (d *Decoder[DF]) GetStreamDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (*StreamDecoder, error) {
	return xsync.DoA2R2(ctx, &d.Locker, d.getStreamDecoder, ctx, stream)
}

func (d *Decoder[DF]) getStreamDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (*StreamDecoder, error) {
	decoder := d.Decoders[stream.Index()]
	logger.Tracef(ctx, "decoder == %v", decoder)
	if decoder != nil {
		return decoder, nil
	}
	rawDecoder, err := d.DecoderFactory.NewDecoder(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize a decoder for stream %d: %w", stream.Index(), err)
	}
	assert(ctx, rawDecoder != nil)
	decoder = &StreamDecoder{
		Decoder: rawDecoder,
	}
	logger.Tracef(ctx, "initialized a decoder: %s", decoder)
	d.Decoders[stream.Index()] = decoder
	return decoder, nil
}

func (d *Decoder[DF]) sendBlankFrameForDroppedPacket(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
	decoder *codec.DecoderLocked,
	streamInfo *frame.StreamInfo,
	packetInfo packetInfo,
	isKeyFrame bool,
) (_err error) {
	logger.Debugf(ctx, "sendBlankFrameForDroppedPacket")
	defer func() { logger.Debugf(ctx, "/sendBlankFrameForDroppedPacket: %v", _err) }()

	codecParams := astiav.AllocCodecParameters()
	defer codecParams.Free()
	decoder.ToCodecParameters(codecParams)
	f, err := frame.NewBlankVideo(ctx, codecParams)
	if err != nil {
		return fmt.Errorf("unable to create a blank frame: %w", err)
	}

	f.SetTimeBase(streamInfo.TimeBase)
	f.SetPts(packetInfo.PTS)
	f.SetPktDts(packetInfo.DTS)
	f.SetDuration(packetInfo.Duration)
	f.SetFlags(f.Flags().Add(astiav.FrameFlagCorrupt))
	if isKeyFrame {
		f.SetFlags(f.Flags().Add(astiav.FrameFlagKey))
	}
	err = d.send(ctx, outputCh, f, streamInfo)
	if err != nil {
		return fmt.Errorf("unable to send a blank frame: %w", err)
	}

	return nil
}

func (d *Decoder[DF]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	pkt, _ := input.Unwrap()
	if pkt == nil {
		return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
	}
	return d.sendPacket(ctx, *pkt, outputCh)
}

func (d *Decoder[DF]) sendPacket(
	ctx context.Context,
	input packet.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sendPacket")
	defer func() { logger.Tracef(ctx, "/sendPacket: %v", _err) }()
	if d.IsClosed() {
		return io.ErrClosedPipe
	}

	streamDecoder, err := xsync.DoR2(ctx, &d.Locker, func() (*StreamDecoder, error) {
		return d.getStreamDecoder(ctx, input.Stream)
	})
	if err != nil {
		return fmt.Errorf("unable to get a stream decoder: %w", err)
	}
	ctx = belt.WithField(ctx, "decoder", streamDecoder)

	if decoderDebug {
		logger.Tracef(ctx, "input packet: dur:%d", input.GetDuration())
	}

	timeBase := input.GetTimeBase()
	if timeBase.Num() == 0 {
		return fmt.Errorf("internal error: TimeBase is not set")
	}

	if !encoderForceCopyTime {
		input.RescaleTs(input.GetTimeBase(), streamDecoder.TimeBase())
	}

	streamIndex := input.GetStreamIndex()

	return streamDecoder.LockDo(ctx, func(
		ctx context.Context,
		decoder *codec.DecoderLocked,
	) (_err error) {
		if decoder.Codec == nil {
			logger.Errorf(ctx, "the decoder is closed; dropping the packet")
			return nil
		}

		var streamInfo *frame.StreamInfo
		{
			var ok bool
			if streamInfo, ok = d.StreamInfo.Load(streamIndex); !ok {
				streamInfo = &frame.StreamInfo{
					Source:           d.asSource(streamDecoder.Decoder),
					CodecParameters:  d.getOutputCodecParameters(ctx, streamIndex, decoder),
					StreamIndex:      streamIndex,
					StreamsCount:     sourceNbStreams(ctx, input.GetSource()),
					TimeBase:         timeBase,
					Duration:         input.GetDuration(),
					PipelineSideData: nil,
				}
				d.StreamInfo.Store(streamIndex, streamInfo)
			}
		}

		packetInfo := packetInfo{
			PTS:         input.GetPTS(),
			DTS:         input.GetDTS(),
			Duration:    input.Packet.Duration(),
			StreamIndex: streamIndex,
			Flags:       packetInfoFlagsFromPipelineSideData(input.PipelineSideData),
		}

		input.Packet.SetOpaque(packetInfo.Bytes())

		for tryCount := 0; ; tryCount++ {
			logger.Tracef(ctx, "decoder.SendPacket(): sending a packet (pts=%d, dts=%d, dur=%d)", input.Packet.Pts(), input.Packet.Dts(), input.Packet.Duration())
			err := decoder.SendPacket(ctx, input.Packet)
			logger.Tracef(ctx, "/decoder.SendPacket(): %v", err)
			shouldRetry := false
			switch {
			case err == nil:
				d.SentPacketsWithoutDecodingFrames++
			case errors.Is(err, astiav.ErrEagain):
				logger.Tracef(ctx, "the decoder is not ready to accept new packets; draining and retrying")
				shouldRetry = true
			case errors.Is(err, codec.ErrNotKeyFrame{}):
				logger.Debugf(ctx, "the packet is not a keyframe and the decoder cannot decode it; dropping the packet")
				if decoderSendBlankFrames && d.AllowBlankFrames.Load() {
					err := d.sendBlankFrameForDroppedPacket(
						ctx,
						outputCh,
						decoder,
						streamInfo,
						packetInfo,
						!streamDecoder.SentBlankKeyFrame,
					)
					if err != nil {
						return fmt.Errorf("unable to send a blank frame for dropped non-keyframe packet: %w", err)
					}
					streamDecoder.SentBlankKeyFrame = true
				}
				return nil
			default:
				return fmt.Errorf("unable to decode the packet: %w", err)
			}

			drainFn := decoder.Drain
			if input.PipelineSideData.Contains(SideFlagFlush{}) {
				logger.Debugf(ctx, "flushing the decoder after sending the packet due to SideFlagFlush")
				drainFn = decoder.Flush
			}
			if err := d.drain(ctx, outputCh, drainFn, packetInfo); err != nil {
				return fmt.Errorf("unable to drain the decoder: %w", err)
			}
			if !shouldRetry {
				break
			}
			if tryCount > 10 {
				return fmt.Errorf("too many retries to send the packet to the decoder: err: %w", err)
			}
		}

		if enableAntiStucking {
			if d.SentPacketsWithoutDecodingFrames > 30 {
				logger.Errorf(ctx, "decoder seems to be stuck, resetting it")
				d.SentPacketsWithoutDecodingFrames = 0
				decoder.Reset(ctx)
			}
		}

		return nil
	})
}

func (d *Decoder[DF]) drain(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
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
	) (_err error) {
		ctx = belt.WithField(ctx, "decoder", decoder)
		opaque := f.Opaque()
		logger.Tracef(ctx, "decoder.ReceiveFrame(): received a frame (opaque len: %d)", len(opaque))

		d.SentPacketsWithoutDecodingFrames = 0

		f.SetPictureType(astiav.PictureTypeNone)

		if len(opaque) > 0 {
			packetInfoPtr := packetInfoFromBytes(f.Opaque())
			if packetInfoPtr == nil {
				return fmt.Errorf("unable to get packet info from frame opaque data: %v", f.Opaque())
			}
			packetInfo = *packetInfoPtr
		}
		logger.Tracef(ctx, "decoder.ReceiveFrame(): packetInfo: %+v", packetInfo)

		streamInfo, ok := d.StreamInfo.Load(packetInfo.StreamIndex)
		if !ok {
			return fmt.Errorf("unable to find stream info for stream index %d", packetInfo.StreamIndex)
		}

		if f.Pts() == astiav.NoPtsValue {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame PTS from packet PTS: %d", packetInfo.PTS)
			}
			f.SetPts(packetInfo.PTS)
			if f.Pts() == astiav.NoPtsValue {
				logger.Warnf(ctx, "frame PTS is not set: %d (packetInfo: %#+v)", f.Pts(), packetInfo)
			}
		}

		if f.Duration() <= 0 {
			if decoderDebug {
				logger.Tracef(ctx, "setting frame duration from packet duration: %d", packetInfo.Duration)
			}
			f.SetDuration(packetInfo.Duration)
			if f.Duration() <= 0 {
				logger.Warnf(ctx, "frame duration is not set: %d (packetInfo: %#+v)", f.Duration(), packetInfo)
			}
		}

		if packetInfo.Flags != 0 {
			streamInfo = frame.BuildStreamInfo(streamInfo.Source, streamInfo.CodecParameters, streamInfo.StreamIndex, streamInfo.StreamsCount, streamInfo.TimeBase, streamInfo.Duration, packetInfo.Flags.PipelineSideData())
		}

		err := d.send(ctx, outputCh, f, streamInfo)
		if err != nil {
			return fmt.Errorf("unable to send decoded frame: %w", err)
		}

		return nil
	})
}

func (d *Decoder[DF]) send(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
	f *astiav.Frame,
	streamInfo *frame.StreamInfo,
) (_err error) {
	frameToSend := frame.BuildOutput(
		f,
		streamInfo,
	)
	logger.Tracef(ctx, "sending a %s frame", frameToSend.GetMediaType())
	defer func() { logger.Tracef(ctx, "/sending a %s frame: %v", frameToSend.GetMediaType(), _err) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputCh <- packetorframe.OutputUnion{Frame: &frameToSend}:
	}
	return nil
}

type decoderAsSource[DF codec.DecoderFactory] struct {
	Kernel  *Decoder[DF]
	Decoder *codec.Decoder
}

var (
	_ frame.Source       = (*decoderAsSource[codec.DecoderFactory])(nil)
	_ codec.GetDecoderer = (*decoderAsSource[codec.DecoderFactory])(nil)
)

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
	logger.Debugf(ctx, "extraData: %s", extradata.Raw(codecParams.ExtraData()))
	return codecParams
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

func (d *Decoder[DF]) ResetSoft(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetSoft")
	defer func() { logger.Debugf(ctx, "/ResetSoft: %v", _err) }()
	return xsync.DoA1R1(ctx, &d.Locker, d.resetSoft, ctx)
}

func (d *Decoder[DF]) resetSoft(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "resetSoft")
	defer func() { logger.Tracef(ctx, "/resetSoft: %v", _err) }()

	var errs []error
	for streamIndex, decoder := range d.Decoders {
		if err := decoder.Reset(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset the decoder for stream #%d: %w", streamIndex, err))
		}
	}

	return errors.Join(errs...)
}

func (d *Decoder[DF]) ResetHard(
	ctx context.Context,
	opts ...CodecResetOption,
) (_err error) {
	logger.Debugf(ctx, "ResetHard")
	defer func() { logger.Debugf(ctx, "/ResetHard: %v", _err) }()
	return xsync.DoA2R1(ctx, &d.Locker, d.resetHard, ctx, opts)
}

func (d *Decoder[DF]) resetHard(
	ctx context.Context,
	opts CodecResetOptions,
) (_err error) {
	cfg := opts.config()
	logger.Tracef(ctx, "resetHard: cfg=%+v", cfg)
	defer func() { logger.Tracef(ctx, "/resetHard: cfg=%#+v: %v", cfg, _err) }()

	var errs []error
	for streamIndex, decoder := range d.Decoders {
		decoder.LockDo(ctx, func(ctx context.Context, decoder *codec.DecoderLocked) (_err error) {
			if err := decoder.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("unable to close the decoder for stream #%d: %w", streamIndex, err))
			}
			delete(d.Decoders, streamIndex)
			delete(d.OutputCodecParameters, streamIndex)
			return nil
		})
	}

	if err := d.DecoderFactory.Reset(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to hard reset the decoder factory: %w", err))
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
	logger.Tracef(ctx, "isDirty")
	defer func() { logger.Tracef(ctx, "/isDirty: %v", _ret) }()
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
	outputCh chan<- packetorframe.OutputUnion,
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
					err := d.drain(
						ctx,
						outputCh,
						decoder.Flush,
						packetInfo{
							StreamIndex: streamIndex,
						},
					)
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
