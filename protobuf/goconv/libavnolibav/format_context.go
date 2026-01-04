// format_context.go provides conversion functions for format context between Protobuf and Go.

package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FormatContext libav_proto.FormatContext

func FormatContextFromProtobuf(input *libav_proto.FormatContext) *FormatContext {
	return (*FormatContext)(input)
}

func (fmtCtx *FormatContext) Protobuf() *libav_proto.FormatContext {
	return (*libav_proto.FormatContext)(fmtCtx)
}
