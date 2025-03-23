package types

import (
	"github.com/asticode/go-astiav"
)

type OutputPacket struct {
	PacketCommons
}

func (o *OutputPacket) UnrefAndFree() {
	o.PacketCommons.Packet.Unref()
	o.PacketCommons.Packet.Free()
}

func BuildOutputPacket(
	pkt *astiav.Packet,
	fmt *astiav.FormatContext,
) OutputPacket {
	return OutputPacket{
		PacketCommons: PacketCommons{
			Packet:        pkt,
			FormatContext: fmt,
		},
	}
}
