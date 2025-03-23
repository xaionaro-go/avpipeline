package types

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type PacketCommons struct {
	*astiav.Packet
	*astiav.FormatContext
}

func (pkt *PacketCommons) PtsAsDuration() time.Duration {
	return avconv.Duration(pkt.Pts(), pkt.TimeBase())
}
