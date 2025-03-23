package types

import (
	"github.com/asticode/go-astiav"
)

type PacketCommons struct {
	*astiav.Packet
	*astiav.FormatContext
}
