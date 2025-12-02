package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type ChannelLayout = libavnolibav.ChannelLayout

func ChannelLayoutFromProtobuf(input *libav_proto.ChannelLayout) *ChannelLayout {
	return libavnolibav.ChannelLayoutFromProtobuf(input)
}

func ChannelLayoutFromGo(input *astiav.ChannelLayout) *ChannelLayout {
	if input == nil {
		return nil
	}
	return &ChannelLayout{
		//Order:      input.Order(),
		NbChannels: int32(input.Channels()),
		//U:          int64(input.U()),
	}
}
