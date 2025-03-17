package avpipeline

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type FilterThrottle struct {
	AverageBitRate         uint64
	BitrateAveragingPeriod time.Duration

	locker                      sync.Mutex
	skippedVideoFrame           bool
	videoAveragerBufferConsumed int64
	prevEncodeTS                time.Time
	isClosed                    bool
	inputChan                   chan InputPacket
	outputChan                  chan OutputPacket
	errorChan                   chan error
	lastSeenFormat              *astiav.FormatContext
}

var _ Filter = (*FilterThrottle)(nil)

func NewFilterThrottle(
	ctx context.Context,
	averageBitRate uint64,
	bitrateAveragingPeriod time.Duration,
) (*FilterThrottle, error) {
	if averageBitRate != 0 && bitrateAveragingPeriod == 0 {
		bitrateAveragingPeriod = time.Second * 10
		logger.Warnf(ctx, "AveragingPeriod is not set, defaulting to %v", bitrateAveragingPeriod)
	}

	f := &FilterThrottle{
		AverageBitRate:         averageBitRate,
		BitrateAveragingPeriod: bitrateAveragingPeriod,
		inputChan:              make(chan InputPacket, 100),
		outputChan:             make(chan OutputPacket, 1),
		errorChan:              make(chan error, 1),
	}

	observability.Go(ctx, func() {
		f.errorChan <- f.readerLoop(ctx)
	})
	return f, nil
}

func (f *FilterThrottle) readerLoop(
	ctx context.Context,
) error {
	return ReaderLoop(ctx, f.inputChan, f)
}

func (f *FilterThrottle) Close() error {
	f.locker.Lock()
	defer f.locker.Unlock()
	close(f.outputChan)
	f.isClosed = true
	return nil
}

func (f *FilterThrottle) SendPacketChan() chan<- InputPacket {
	return f.inputChan
}

func (f *FilterThrottle) SendPacket(
	ctx context.Context,
	input InputPacket,
) (_err error) {
	f.locker.Lock()
	defer f.locker.Unlock()
	f.lastSeenFormat = input.FormatContext

	mediaType := input.Stream.CodecParameters().MediaType()
	switch mediaType {
	case astiav.MediaTypeVideo:
		return f.sendPacketVideo(ctx, input)
	case astiav.MediaTypeAudio:
		return f.sendPacketAudio(ctx, input)
	default:
		logger.Tracef(ctx, "an uninteresting packet of type %s", mediaType)
		// we don't care about everything else
		return nil
	}
}

func (f *FilterThrottle) sendPacketVideo(
	ctx context.Context,
	input InputPacket,
) error {
	if f.isClosed {
		return io.ErrClosedPipe
	}

	if f.AverageBitRate == 0 {
		f.videoAveragerBufferConsumed = 0
		f.outputChan <- OutputPacket{
			Packet: ClonePacketAsReferenced(input.Packet),
		}
		return nil
	}

	now := time.Now()
	prevTS := f.prevEncodeTS
	f.prevEncodeTS = now

	tsDiff := now.Sub(prevTS)
	allowMoreBits := 1 + int64(tsDiff.Seconds()*float64(f.AverageBitRate))

	f.videoAveragerBufferConsumed -= allowMoreBits
	if f.videoAveragerBufferConsumed < 0 {
		f.videoAveragerBufferConsumed = 0
	}

	pktSize := input.Packet.Size()
	averagingBuffer := int64(f.BitrateAveragingPeriod.Seconds() * float64(f.AverageBitRate))
	consumedWithPacket := f.videoAveragerBufferConsumed + int64(pktSize)*8
	if consumedWithPacket > averagingBuffer {
		f.skippedVideoFrame = true
		logger.Tracef(ctx, "skipping a frame to reduce the bitrate: %d > %d", consumedWithPacket, averagingBuffer)
		return nil
	}

	if f.skippedVideoFrame {
		isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)
		if !isKeyFrame {
			logger.Tracef(ctx, "skipping a non-key frame (BTW, the consumedWithPacket is %d/%d)", consumedWithPacket, averagingBuffer)
			return nil
		}
	}

	f.skippedVideoFrame = false
	f.videoAveragerBufferConsumed = consumedWithPacket
	f.outputChan <- OutputPacket{
		Packet: ClonePacketAsReferenced(input.Packet),
	}
	return nil
}

func (f *FilterThrottle) sendPacketAudio(
	ctx context.Context,
	input InputPacket,
) (_err error) {
	logger.Tracef(
		ctx,
		"an audio packet (pos:%d, pts:%d, dts:%d, dur:%d)",
		input.Packet.Pos(), input.Packet.Pts(), input.Packet.Dts(), input.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "an audio packet: %v", _err) }()
	f.outputChan <- OutputPacket{
		Packet: ClonePacketAsReferenced(input.Packet),
	}
	return nil
}

func (f *FilterThrottle) OutputPacketsChan() <-chan OutputPacket {
	return f.outputChan
}

func (f *FilterThrottle) ErrorChan() <-chan error {
	return f.errorChan
}

func (f *FilterThrottle) GetOutputFormatContext(_ context.Context) *astiav.FormatContext {
	f.locker.Lock()
	defer f.locker.Unlock()
	return f.lastSeenFormat
}

func (f *FilterThrottle) String() string {
	return "FilterThrottle"
}

func (f *FilterThrottle) LockDo(fn func()) {
	f.locker.Lock()
	defer f.locker.Unlock()
	fn()
}
