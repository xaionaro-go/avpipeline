// codec_parameters.go provides conversion functions for codec parameters between Protobuf and Go.

package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type CodecParameters libav_proto.CodecParameters

func CodecParametersFromProtobuf(input *libav_proto.CodecParameters) *CodecParameters {
	return (*CodecParameters)(input)
}

func (f *CodecParameters) Protobuf() *libav_proto.CodecParameters {
	return (*libav_proto.CodecParameters)(f)
}
