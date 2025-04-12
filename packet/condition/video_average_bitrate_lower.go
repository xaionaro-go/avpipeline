package condition

import (
	"context"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
)

type VideoAverageBitrateLower struct {
	AverageBitRate         uint64
	BitrateAveragingPeriod time.Duration

	skippedVideoFrame           bool
	videoAveragerBufferConsumed int64
	prevEncodeTS                time.Time
	isClosed                    bool
	lastSeenFormatAccessor      packet.Source
}

var _ Condition = (*VideoAverageBitrateLower)(nil)

func NewVideoAverageBitrateLower(
	ctx context.Context,
	averageBitRate uint64,
	bitrateAveragingPeriod time.Duration,
) *VideoAverageBitrateLower {
	if averageBitRate != 0 && bitrateAveragingPeriod == 0 {
		bitrateAveragingPeriod = time.Second * 10
		logger.Warnf(ctx, "AveragingPeriod is not set, defaulting to %v", bitrateAveragingPeriod)
	}

	return &VideoAverageBitrateLower{
		AverageBitRate:         averageBitRate,
		BitrateAveragingPeriod: bitrateAveragingPeriod,
	}
}

func (f *VideoAverageBitrateLower) Match(
	ctx context.Context,
	input packet.Input,
) bool {
	if f.AverageBitRate == 0 {
		return true
	}
	f.lastSeenFormatAccessor = input.Source

	mediaType := input.Stream.CodecParameters().MediaType()
	switch mediaType {
	case astiav.MediaTypeVideo:
		return f.sendInputVideo(ctx, input)
	default:
		logger.Tracef(ctx, "an uninteresting packet of type %s", mediaType)
		// we don't care about everything else
		return true
	}
}

func (f *VideoAverageBitrateLower) sendInputVideo(
	ctx context.Context,
	input packet.Input,
) bool {
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
		return false
	}

	if f.skippedVideoFrame {
		isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)
		if !isKeyFrame {
			logger.Tracef(ctx, "skipping a non-key frame (BTW, the consumedWithPacket is %d/%d)", consumedWithPacket, averagingBuffer)
			return false
		}
	}

	f.skippedVideoFrame = false
	f.videoAveragerBufferConsumed = consumedWithPacket
	return true
}

func (f *VideoAverageBitrateLower) String() string {
	return fmt.Sprintf(
		"VideoAverageBitrateLower(%v, %v)",
		f.AverageBitRate,
		f.BitrateAveragingPeriod,
	)
}
