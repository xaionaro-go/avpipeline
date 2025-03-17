package types

import (
	"github.com/asticode/go-astiav"
)

type InputPacket struct {
	*astiav.Packet
	*astiav.Stream
	*astiav.FormatContext
}
