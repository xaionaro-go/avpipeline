package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FormatContext libav_proto.FormatContext

func FormatContextFromProtobuf(input *libav_proto.FormatContext) *FormatContext {
	return (*FormatContext)(input)
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

func (fmtCtx *FormatContext) Protobuf() *libav_proto.FormatContext {
	return (*libav_proto.FormatContext)(fmtCtx)
}

func (fmtCtx *FormatContext) Go() *astiav.FormatContext {
	if fmtCtx == nil {
		return nil
	}
	panic("not implemented")
}
