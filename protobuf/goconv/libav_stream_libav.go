//go:build with_libav
// +build with_libav

package goconv

import (
	"github.com/asticode/go-astiav"
)

func StreamFromGo(input *astiav.Stream) *Stream {
	if input == nil {
		return nil
	}
	return &Stream{
		Index:             int32(input.Index()),
		CodecParameters:   CodecParametersFromGo(input.CodecParameters()).Protobuf(),
		TimeBase:          RationalFromGo(ptr(input.TimeBase())).Protobuf(),
		StartTime:         input.StartTime(),
		Duration:          input.Duration(),
		NbFrames:          input.NbFrames(),
		Disposition:       int32(input.DispositionFlags()),
		Discard:           int32(input.Discard()),
		SampleAspectRatio: RationalFromGo(ptr(input.SampleAspectRatio())).Protobuf(),
		//Metadata:          DictionaryFromGo(input.Metadata()).Protobuf(),
		AvgFrameRate: RationalFromGo(ptr(input.AvgFrameRate())).Protobuf(),
		//AttachedPic: PacketFromGo(input.AttachedPic(), false).Protobuf(),
		//SideData: StreamSideDataFromGo(input.SideData()).Protobuf(),
		EventFlags: int32(input.EventFlags()),
		RFrameRate: RationalFromGo(ptr(input.RFrameRate())).Protobuf(),
		//PtsWrapBits: int32(input.PtsWrapBits()),
	}
}

func (s *Stream) Go() *astiav.Stream {
	if s == nil {
		return nil
	}
	panic("not implemented")
}
