package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Stream libav_proto.Stream

func StreamFromProtobuf(input *libav_proto.Stream) *Stream {
	return (*Stream)(input)
}

func StreamFromGo(input *astiav.Stream) *Stream {
	if input == nil {
		return nil
	}
	return &Stream{
		Index:             int32(input.Index()),
		CodecParameters:   &libav_proto.CodecParameters{},
		TimeBase:          &libav_proto.Rational{},
		StartTime:         0,
		Duration:          0,
		NbFrames:          0,
		Disposition:       0,
		Discard:           0,
		SampleAspectRatio: &libav_proto.Rational{},
		Metadata:          &libav_proto.Dictionary{},
		AvgFrameRate:      &libav_proto.Rational{},
		AttachedPic:       &libav_proto.Packet{},
		SideData:          &libav_proto.SideData{},
		EventFlags:        0,
		RFrameRate:        &libav_proto.Rational{},
		PtsWrapBits:       0,
	}
}

func (s *Stream) Protobuf() *libav_proto.Stream {
	return (*libav_proto.Stream)(s)
}

func (s *Stream) Go() *astiav.Stream {
	if s == nil {
		return nil
	}
	panic("not implemented")
}
