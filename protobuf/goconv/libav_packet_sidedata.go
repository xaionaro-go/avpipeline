package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type PacketSideData libav_proto.PacketSideData

func PacketSideDataFromProtobuf(input *libav_proto.PacketSideData) *PacketSideData {
	return (*PacketSideData)(input)
}

func PacketSideDataFromGo(input *astiav.PacketSideData) *PacketSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}

func (f *PacketSideData) Protobuf() *libav_proto.PacketSideData {
	return (*libav_proto.PacketSideData)(f)
}

func (f *PacketSideData) Go() *astiav.PacketSideData {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
