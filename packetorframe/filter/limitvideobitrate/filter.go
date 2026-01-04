// filter.go implements a filter that limits the video bitrate.

// Package limitvideobitrate provides a filter that limits the video bitrate.
package limitvideobitrate

import (
	"context"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type Filter struct {
	AverageBitRate         uint64
	BitrateAveragingPeriod time.Duration

	skippedVideoFrame           bool
	videoAveragerBufferConsumed int64
	prevEncodeTS                time.Time
	lastSeenFormatAccessor      packetorframe.AbstractSource
}

var _ condition.Condition = (*Filter)(nil)

func New(
	ctx context.Context,
	averageBitRate uint64,
	bitrateAveragingPeriod time.Duration,
) *Filter {
	if averageBitRate != 0 && bitrateAveragingPeriod == 0 {
		bitrateAveragingPeriod = time.Second * 10
		logger.Warnf(ctx, "AveragingPeriod is not set, defaulting to %v", bitrateAveragingPeriod)
	}

	return &Filter{
		AverageBitRate:         averageBitRate,
		BitrateAveragingPeriod: bitrateAveragingPeriod,
	}
}

func (f *Filter) Match(
	ctx context.Context,
	input packetorframe.InputUnion,
) bool {
	if f.AverageBitRate == 0 {
		return true
	}
	f.lastSeenFormatAccessor = input.GetSource()

	mediaType := input.GetMediaType()
	switch mediaType {
	case astiav.MediaTypeVideo:
		return f.sendInputVideo(ctx, input)
	default:
		logger.Tracef(ctx, "an uninteresting packet of type %s", mediaType)
		// we don't care about everything else
		return true
	}
}

func (f *Filter) sendInputVideo(
	ctx context.Context,
	input packetorframe.InputUnion,
) bool {
	if input.Packet == nil || input.Packet.Packet == nil {
		// This filter only works with packets, not frames
		return true
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

	isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)

	pktSize := input.Packet.Size()
	averagingBuffer := int64(f.BitrateAveragingPeriod.Seconds() * float64(f.AverageBitRate))
	consumedWithPacket := f.videoAveragerBufferConsumed + int64(pktSize)*8
	if consumedWithPacket > averagingBuffer && (!isKeyFrame || f.videoAveragerBufferConsumed != 0) {
		f.skippedVideoFrame = true
		logger.Tracef(ctx, "skipping a frame to reduce the bitrate: %d > %d", consumedWithPacket, averagingBuffer)
		return false
	}

	if f.skippedVideoFrame && !isKeyFrame {
		logger.Tracef(ctx, "skipping a non-key frame (BTW, the consumedWithPacket is %d/%d)", consumedWithPacket, averagingBuffer)
		return false
	}

	f.skippedVideoFrame = false
	f.videoAveragerBufferConsumed = consumedWithPacket
	return true
}

func (f *Filter) String() string {
	return fmt.Sprintf(
		"LimitVideoBitrate(%v, %v)",
		f.AverageBitRate,
		f.BitrateAveragingPeriod,
	)
}
