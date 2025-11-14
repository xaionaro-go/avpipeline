package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FrameSideData libav_proto.FrameSideData

func FrameSideDataFromProtobuf(input *libav_proto.FrameSideData) *FrameSideData {
	return (*FrameSideData)(input)
}

func FrameSideDataFromGo(input *astiav.FrameSideData) *FrameSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}

func (f *FrameSideData) Protobuf() *libav_proto.FrameSideData {
	return (*libav_proto.FrameSideData)(f)
}

func (f *FrameSideData) Go() *astiav.FrameSideData {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
