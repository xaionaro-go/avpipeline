package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type PacketSideData = libavnolibav.PacketSideData

func PacketSideDataFromProtobuf(input *libav_proto.PacketSideData) *PacketSideData {
	return libavnolibav.PacketSideDataFromProtobuf(input)
}

func PacketSideDataFromGo(input *astiav.PacketSideData) *PacketSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}
