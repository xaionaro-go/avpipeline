package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Packet libav_proto.Packet

func PacketFromProtobuf(input *libav_proto.Packet) *Packet {
	return (*Packet)(input)
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

func (f *Packet) Protobuf() *libav_proto.Packet {
	return (*libav_proto.Packet)(f)
}

func (f *Packet) Go() *astiav.Packet {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
