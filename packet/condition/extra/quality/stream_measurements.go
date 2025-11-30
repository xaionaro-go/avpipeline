package quality

import (
	"context"
	"slices"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
)

const (
	maxAggregationPeriod     = time.Second
	maxFPS                   = 240 // assuming 240 FPS is the maximal we will ever use
	maxDTSAndDurationsStored = 1 + int(float64(maxFPS)*float64(maxAggregationPeriod)/float64(time.Second))
)

type StreamMeasurements struct {
	Locker          xsync.Mutex
	MediaType       astiav.MediaType
	TimeBase        astiav.Rational
	DTSAndDurations DTSAndDurations
}

type StreamQualityWithMediaType struct {
	MediaType astiav.MediaType
	StreamQuality
}

func newStreamMeasurements(
	mediaType astiav.MediaType,
	timeBase astiav.Rational,
) *StreamMeasurements {
	return &StreamMeasurements{
		MediaType: mediaType,
		TimeBase:  timeBase,
		DTSAndDurations: DTSAndDurations{
			Latest: ringbuffer.New[DTSAndDuration](maxDTSAndDurationsStored),
		},
	}
}

func (sm *StreamMeasurements) observePacketLocked(
	ctx context.Context,
	packet packet.Input,
) {
	sm.DTSAndDurations.observePacketLocked(ctx, packet)
}

func (sm *StreamMeasurements) getStreamQuality(
	ctx context.Context,
) (_ret *StreamQuality, err error) {
	return xsync.DoA1R2(ctx, &sm.Locker, sm.getStreamQualityLocked, ctx)
}

func (sm *StreamMeasurements) getStreamQualityLocked(
	ctx context.Context,
) (_ret *StreamQuality, err error) {
	r := &StreamQuality{
		Continuity: 0,
		Overlap:    0,
		FrameRate:  0,
		InvalidDTS: 0,
	}

	totalPackets := sm.DTSAndDurations.Latest.Len()
	if totalPackets == 0 {
		return r, nil
	}

	discontinuityLength := int64(0)
	overlapLength := int64(0)
	dtsAndDurations := sm.DTSAndDurations.Latest.GetAll()
	last := dtsAndDurations[len(dtsAndDurations)-1]
	endTSInt := last.DTS + last.Duration
	endTS := avconv.Duration(endTSInt, sm.TimeBase)
	startTS := endTS - maxAggregationPeriod
	minTSInt := endTSInt
	prevDTSInt := last.DTS
	frameCount := 1
	for _, item := range slices.Backward(dtsAndDurations[:len(dtsAndDurations)-1]) {
		if item.DTS >= prevDTSInt {
			r.InvalidDTS++
			continue
		}
		dts := avconv.Duration(item.DTS, sm.TimeBase)
		if dts < startTS {
			break
		}
		frameCount++
		minTSInt = item.DTS
		endTSInt := item.DTS + item.Duration
		discontinuityRange := prevDTSInt - endTSInt
		prevDTSInt = item.DTS
		switch {
		case discontinuityRange > 0:
			discontinuityLength += discontinuityRange
		case discontinuityRange < 0:
			overlapLength += -discontinuityRange
		}
	}
	intervalInt := endTSInt - minTSInt
	r.Continuity = 1 - float64(discontinuityLength)/float64(intervalInt)
	r.Overlap = float64(overlapLength) / float64(intervalInt)
	r.FrameRate = float64(frameCount) / maxAggregationPeriod.Seconds()
	return r, nil
}
