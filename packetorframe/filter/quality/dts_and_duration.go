package quality

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	"tailscale.com/util/ringbuffer"
)

type DTSAndDurations struct {
	Latest *ringbuffer.RingBuffer[DTSAndDuration]
}

type DTSAndDuration struct {
	DTS      int64
	Duration int64
}

func (s *DTSAndDurations) observePacketOrFrameLocked(
	_ context.Context,
	input packetorframe.InputUnion,
) {
	s.Latest.Add(DTSAndDuration{
		DTS:      input.GetDTS(),
		Duration: input.GetDuration(),
	})
}
