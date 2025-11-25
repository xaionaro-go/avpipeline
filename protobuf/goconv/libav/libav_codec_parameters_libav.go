//go:build with_libav
// +build with_libav

package libav

import (
	"github.com/asticode/go-astiav"
)

func CodecParametersFromGo(input *astiav.CodecParameters) *CodecParameters {
	if input == nil {
		return nil
	}
	return &CodecParameters{
		//CodecType: int32(input.CodecType),
		CodecID:  uint32(input.CodecID()),
		CodecTag: uint32(input.CodecTag()),
		//ExtraData:
		//CodedSideData: PacketSideDataFromGo(input.CodedSideData).Protobuf(),
		Format:  int32(input.PixelFormat()),
		BitRate: int64(input.BitRate()),
		//BitsPerCodecSample: int32(input.BitsPerCodecSample()),
		//BitsPerRawSample:   int32(input.BitsPerRawSample()),
		Profile:           int32(input.Profile()),
		Level:             int32(input.Level()),
		Width:             int32(input.Width()),
		Height:            int32(input.Height()),
		SampleAspectRatio: RationalFromGo(ptr(input.SampleAspectRatio())).Protobuf(),
		Framerate:         RationalFromGo(ptr(input.FrameRate())).Protobuf(),
		//FieldOrder:        int32(input.FieldOrder()),
		//ColorRange:        int32(input.ColorRange()),
		//ColorTRC:          int32(input.ColorTRC()),
		//ColorSpace:        int32(input.ColorSpace()),
		//ChromaLocation:    int32(input.ChromaLocation()),
		//VideoDelay:        int64(input.VideoDelay()),
		ChLayout:   ChannelLayoutFromGo(ptr(input.ChannelLayout())).Protobuf(),
		SampleRate: int32(input.SampleRate()),
		//BlockAlign:        int32(input.BlockAlign()),
		FrameSize: int32(input.FrameSize()),
		//InitialPadding:    int64(input.InitialPadding()),
		//TrailingPadding:   int64(input.TrailingPadding()),
		//SeekPreroll:       int64(input.SeekPreroll()),
	}
}

func (f *CodecParameters) Go() *astiav.CodecParameters {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
