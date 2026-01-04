// stream.go provides conversion functions for streams between Protobuf and Go.

package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Stream = libavnolibav.Stream

func StreamFromProtobuf(input *libav_proto.Stream) *Stream {
	return libavnolibav.StreamFromProtobuf(input)
}

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
		// Metadata:          DictionaryFromGo(input.Metadata()).Protobuf(),
		AvgFrameRate: RationalFromGo(ptr(input.AvgFrameRate())).Protobuf(),
		// AttachedPic: PacketFromGo(input.AttachedPic(), false).Protobuf(),
		// SideData: StreamSideDataFromGo(input.SideData()).Protobuf(),
		EventFlags: int32(input.EventFlags()),
		RFrameRate: RationalFromGo(ptr(input.RFrameRate())).Protobuf(),
		// PtsWrapBits: int32(input.PtsWrapBits()),
	}
}
