package packet

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Commons struct {
	*astiav.Packet
	*astiav.Stream
	*astiav.FormatContext
}

func (pkt *Commons) GetStreamIndex() int {
	return pkt.Packet.StreamIndex()
}

func (pkt *Commons) GetStream() *astiav.Stream {
	return pkt.Stream
}

func (pkt *Commons) PtsAsDuration() time.Duration {
	return avconv.Duration(pkt.Pts(), pkt.Stream.TimeBase())
}
