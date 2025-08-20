package packet

import (
	"github.com/asticode/go-astiav"
)

type Input = Commons

func BuildInput(
	pkt *astiav.Packet,
	streamInfo *StreamInfo,
) Input {
	return Input{
		Packet:     pkt,
		StreamInfo: streamInfo,
	}
}
