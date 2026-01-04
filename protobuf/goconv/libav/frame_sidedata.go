// frame_sidedata.go provides conversion functions for frame side data between Protobuf and Go.

package libav

import (
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type FrameSideData = libavnolibav.FrameSideData

func FrameSideDataFromProtobuf(input *libav_proto.FrameSideData) *FrameSideData {
	return libavnolibav.FrameSideDataFromProtobuf(input)
}

/*
func FrameSideDataFromGo(input *astiav.FrameSideData) *FrameSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}
*/
