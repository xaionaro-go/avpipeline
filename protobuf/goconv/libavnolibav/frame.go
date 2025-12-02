package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Frame libav_proto.Frame

func FrameFromProtobuf(input *libav_proto.Frame) *Frame {
	return (*Frame)(input)
}

func (f *Frame) Protobuf() *libav_proto.Frame {
	return (*libav_proto.Frame)(f)
}
