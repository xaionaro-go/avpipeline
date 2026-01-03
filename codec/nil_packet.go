// nil_packet.go provides global nil packet initialization.

package codec

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
)

var nilPacket *astiav.Packet

func init() {
	nilPacket = packet.Pool.Get()
}
