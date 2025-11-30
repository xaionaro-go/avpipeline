package quality

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type StreamKey struct {
	Source packet.Source
	Index  int
}

type Measurements struct {
	Locker            xsync.Mutex
	StreamQualityInfo map[StreamKey]*StreamMeasurements
}

func NewMeasurements() *Measurements {
	return &Measurements{
		StreamQualityInfo: map[StreamKey]*StreamMeasurements{},
	}
}

func (m *Measurements) ObservePacket(
	ctx context.Context,
	packet packet.Input,
) {
	sm := xsync.DoR1(ctx, &m.Locker, func() (_ret *StreamMeasurements) {
		defer func() { _ret.Locker.ManualLock(ctx) }()
		streamKey := StreamKey{
			Source: packet.GetSource(),
			Index:  packet.GetStreamIndex(),
		}
		sm, ok := m.StreamQualityInfo[streamKey]
		if ok {
			return sm
		}
		sm = newStreamMeasurements(packet.GetMediaType(), packet.Stream.TimeBase())
		m.StreamQualityInfo[streamKey] = sm
		return sm
	})
	defer sm.Locker.ManualUnlock(ctx)
	sm.observePacketLocked(ctx, packet)
}

func (m *Measurements) GetQuality(
	ctx context.Context,
) (_ret *Quality, err error) {
	return xsync.DoA1R2(ctx, &m.Locker, m.getQualityLocked, ctx)
}

func (m *Measurements) getQualityLocked(
	ctx context.Context,
) (_ret *Quality, err error) {
	result := Quality{}
	for streamKey, sqi := range m.StreamQualityInfo {
		sq, err := sqi.getStreamQuality(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting stream quality for stream index %d: %w", streamKey, err)
		}
		result = append(result, &StreamQualityWithMediaType{
			MediaType:     sqi.MediaType,
			StreamQuality: *sq,
		})
	}
	return &result, nil
}
