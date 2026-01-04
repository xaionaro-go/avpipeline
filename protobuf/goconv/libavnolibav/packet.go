// packet.go provides conversion functions for packet between Protobuf and Go.

package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Packet libav_proto.Packet

func PacketFromProtobuf(input *libav_proto.Packet) *Packet {
	return (*Packet)(input)
}

func (f *Packet) Protobuf() *libav_proto.Packet {
	return (*libav_proto.Packet)(f)
}
