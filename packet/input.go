package packet

import (
	"github.com/asticode/go-astiav"
)

type Input = Commons

func BuildInput(
	pkt *astiav.Packet,
	s *astiav.Stream,
	fmt *astiav.FormatContext,
) Input {
	pkt.Pos()
	return Input{
		Packet:        pkt,
		Stream:        s,
		FormatContext: fmt,
	}
}
