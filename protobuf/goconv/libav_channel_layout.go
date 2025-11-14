package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type ChannelLayout libav_proto.ChannelLayout

func ChannelLayoutFromProtobuf(input *libav_proto.ChannelLayout) *ChannelLayout {
	return (*ChannelLayout)(input)
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

func (f *ChannelLayout) Protobuf() *libav_proto.ChannelLayout {
	return (*libav_proto.ChannelLayout)(f)
}

func (f *ChannelLayout) Go() *astiav.ChannelLayout {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
