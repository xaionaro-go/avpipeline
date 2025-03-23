package types

import (
	"github.com/asticode/go-astiav"
)

type InputPacket struct {
	PacketCommons
	*astiav.Stream
}

func BuildInputPacket(
	pkt *astiav.Packet,
	fmt *astiav.FormatContext,
	s *astiav.Stream,
) InputPacket {
	return InputPacket{
		PacketCommons: PacketCommons{
			Packet:        pkt,
			FormatContext: fmt,
		},
		Stream: s,
	}
}
