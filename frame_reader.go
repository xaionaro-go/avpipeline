package avpipeline

import (
	"github.com/asticode/go-astiav"
)

type InputPacket struct {
	*astiav.Packet
	*astiav.Stream
	*astiav.FormatContext
}

type OutputPacket struct {
	*astiav.Packet
}

func (o *OutputPacket) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}
