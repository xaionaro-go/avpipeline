package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type ChannelLayout libav_proto.ChannelLayout

func ChannelLayoutFromProtobuf(input *libav_proto.ChannelLayout) *ChannelLayout {
	return (*ChannelLayout)(input)
}

func (f *ChannelLayout) Protobuf() *libav_proto.ChannelLayout {
	return (*libav_proto.ChannelLayout)(f)
}
