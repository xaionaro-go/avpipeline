package quality

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
	"tailscale.com/util/ringbuffer"
)

type DTSAndDurations struct {
	Latest *ringbuffer.RingBuffer[DTSAndDuration]
}

type DTSAndDuration struct {
	DTS      int64
	Duration int64
}

func (s *DTSAndDurations) observePacketLocked(
	_ context.Context,
	packet packet.Input,
) {
	s.Latest.Add(DTSAndDuration{
		DTS:      packet.GetDTS(),
		Duration: packet.GetDuration(),
	})
}
