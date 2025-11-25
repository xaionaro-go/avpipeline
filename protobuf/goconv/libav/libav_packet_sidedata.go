package libav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type PacketSideData libav_proto.PacketSideData

func PacketSideDataFromProtobuf(input *libav_proto.PacketSideData) *PacketSideData {
	return (*PacketSideData)(input)
}

func (f *PacketSideData) Protobuf() *libav_proto.PacketSideData {
	return (*libav_proto.PacketSideData)(f)
}
