// stream.go provides conversion functions for stream between Protobuf and Go.

package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Stream libav_proto.Stream

func StreamFromProtobuf(input *libav_proto.Stream) *Stream {
	return (*Stream)(input)
}

func (s *Stream) Protobuf() *libav_proto.Stream {
	return (*libav_proto.Stream)(s)
}
