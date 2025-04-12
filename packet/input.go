package packet

import (
	"github.com/asticode/go-astiav"
)

type Input = Commons

func BuildInput(
	pkt *astiav.Packet,
	s *astiav.Stream,
	fmt Source,
) Input {
	pkt.Pos()
	return Input{
		Packet: pkt,
		Stream: s,
		Source: fmt,
	}
}
