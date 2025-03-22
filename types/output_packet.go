package types

import (
	"github.com/asticode/go-astiav"
)

type OutputPacket struct {
	*astiav.Packet
}

func (o *OutputPacket) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}
