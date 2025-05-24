package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Frame libav_proto.Frame

func FrameFromProtobuf(input *libav_proto.Frame) *Frame {
	return (*Frame)(input)
}

func FrameFromGo(input *astiav.Frame) *Frame {
	if input == nil {
		return nil
	}
	panic("not implemented")
}

func (f *Frame) Protobuf() *libav_proto.Frame {
	return (*libav_proto.Frame)(f)
}

func (f *Frame) Go() *astiav.Frame {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
