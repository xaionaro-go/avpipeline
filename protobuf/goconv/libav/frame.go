package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Frame = libavnolibav.Frame

func FrameFromProtobuf(input *libav_proto.Frame) *Frame {
	return libavnolibav.FrameFromProtobuf(input)
}

func FrameFromGo(input *astiav.Frame, includePayload bool) *Frame {
	if input == nil {
		return nil
	}
	var keyFrame int32
	if input.KeyFrame() {
		keyFrame = 1
	}
	data, _ := input.Data().Bytes(0)
	f := &Frame{
		DataSize:          uint32(len(data)),
		Linesize:          LinesizeFromGo(input.Linesize()).Protobuf(),
		Width:             int32(input.Width()),
		Height:            int32(input.Height()),
		NbSamples:         int32(input.NbSamples()),
		Format:            int32(input.SampleFormat()),
		KeyFrame:          keyFrame,
		PictType:          uint32(input.PictureType()),
		SampleAspectRatio: RationalFromGo(ptr(input.SampleAspectRatio())).Protobuf(),
		Pts:               input.Pts(),
		PktDts:            input.PktDts(),
		TimeBase:          RationalFromGo(ptr(input.TimeBase())).Protobuf(),
		Quality:           int32(input.Quality()),
		//RepeatPict:        input.RepeatPicture(),
		//InterlacedFrame:   input.InterlacedFrame(),
		//TopFieldFirst:     input.TopFieldFirst(),
		//PaletteHasChanged: input.PaletteHasChanged(),
		SampleRate: int32(input.SampleRate()),
		SideData:   FrameSideDataFromGo(input.SideData()).Protobuf(),
		Flags:      int32(input.Flags()),
		ColorRange: uint32(input.ColorRange()),
		//ColorTRC:          input.ColorTRC(),
		ColorSpace: uint32(input.ColorSpace()),
		//ChromaLocation:      uint32(input.ChromaLocation()),
		//BestEffortTimestamp: input.BestEffortTimestamp(),
		//PktPos:              input.PktPos(),
		//Metadata:            DictionaryFromGo(input.Metadata()).Protobuf(),
		//DecodeErrorFlags:    input.DecodeErrorFlags(),
		//PktSize:             input.PktSize(),
		//CropTop:             input.CropTop(),
		//CropBottom:          input.CropBottom(),
		//CropLeft:            input.CropLeft(),
		//CropRight:           input.CropRight(),
		ChLayout: ChannelLayoutFromGo(ptr(input.ChannelLayout())).Protobuf(),
		Duration: input.Duration(),
	}
	if includePayload {
		f.Data = data
		//f.ExtendedData = input.ExtendedData()
	}
	return f
}
