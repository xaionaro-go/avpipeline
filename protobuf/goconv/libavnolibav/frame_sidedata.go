// frame_sidedata.go provides conversion functions for frame side data between Protobuf and Go.

package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FrameSideData libav_proto.FrameSideData

func FrameSideDataFromProtobuf(input *libav_proto.FrameSideData) *FrameSideData {
	return (*FrameSideData)(input)
}

func (f *FrameSideData) Protobuf() *libav_proto.FrameSideData {
	return (*libav_proto.FrameSideData)(f)
}
