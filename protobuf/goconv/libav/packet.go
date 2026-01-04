// packet.go provides conversion functions for packets between Protobuf and Go.

package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Packet = libavnolibav.Packet

func PacketFromProtobuf(input *libav_proto.Packet) *Packet {
	return libavnolibav.PacketFromProtobuf(input)
}

func PacketFromGo(input *astiav.Packet, includePayload bool) *Packet {
	if input == nil {
		return nil
	}
	pkt := &Packet{
		Pts:         input.Pts(),
		Dts:         input.Dts(),
		DataSize:    uint32(len(input.Data())),
		StreamIndex: int32(input.StreamIndex()),
		Flags:       uint32(input.Flags()),
		SideData:    PacketSideDataFromGo(input.SideData()).Protobuf(),
		Duration:    input.Duration(),
		Pos:         input.Pos(),
		TimeBase:    RationalFromGo(ptr(input.TimeBase())).Protobuf(),
	}
	if includePayload {
		pkt.Data = input.Data()
	}
	return pkt
}
