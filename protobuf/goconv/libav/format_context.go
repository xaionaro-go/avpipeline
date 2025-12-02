package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FormatContext = libavnolibav.FormatContext

func FormatContextFromProtobuf(input *libav_proto.FormatContext) *FormatContext {
	return libavnolibav.FormatContextFromProtobuf(input)
}

func FormatContextFromGo(input *astiav.FormatContext) *FormatContext {
	if input == nil {
		return nil
	}
	result := &FormatContext{}
	for _, stream := range input.Streams() {
		result.Streams = append(result.Streams, StreamFromGo(stream).Protobuf())
	}
	return result
}
