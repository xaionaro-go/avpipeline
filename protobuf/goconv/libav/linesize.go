// linesize.go provides conversion functions for linesize between Protobuf and Go.

// Package libav provides conversion functions between Protobuf and Go for libav types.
package libav

import (
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
)

type Linesize = libavnolibav.Linesize

func LinesizeFromProtobuf(input []uint32) Linesize {
	return libavnolibav.LinesizeFromProtobuf(input)
}

func LinesizeFromGo(input [8]int) Linesize {
	return libavnolibav.LinesizeFromGo(input)
}
