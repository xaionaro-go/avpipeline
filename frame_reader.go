package avpipeline

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

type InputPacket = types.InputPacket

type OutputPacket struct {
	*astiav.Packet
}

func (o *OutputPacket) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}
