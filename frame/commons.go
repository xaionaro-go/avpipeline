package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type StreamInfo struct {
	Source           Source
	CodecParameters  *astiav.CodecParameters // TODO: remove this from here
	StreamIndex      int
	StreamsCount     int
	TimeBase         astiav.Rational
	Duration         int64
	PipelineSideData types.PipelineSideData
}

func BuildStreamInfo(
	source Source,
	codecParameters *astiav.CodecParameters,
	streamIndex, streamsCount int,
	timeBase astiav.Rational,
	duration int64,
	pipelineSideData types.PipelineSideData,
) *StreamInfo {
	return &StreamInfo{
		Source:           source,
		CodecParameters:  codecParameters,
		StreamIndex:      streamIndex,
		StreamsCount:     streamsCount,
		TimeBase:         timeBase,
		Duration:         duration,
		PipelineSideData: pipelineSideData,
	}
}

type Commons struct {
	*astiav.Frame
	Pos int64 // TODO: reuse pkt_pos from the frame
	*StreamInfo
}

func (f *Commons) GetMediaType() astiav.MediaType {
	return f.CodecParameters.MediaType()
}

func (f *Commons) GetTimeBase() astiav.Rational {
	return f.TimeBase
}

func (f *Commons) GetSize() int {
	return 0 // TODO: fix this
}

func (f *Commons) GetStreamIndex() int {
	return f.StreamIndex
}

func (f *Commons) GetDurationAsDuration() time.Duration {
	return avconv.Duration(f.StreamInfo.Duration, f.TimeBase)
}

func (f *Commons) GetDTSAsDuration() time.Duration {
	return avconv.Duration(f.PktDts(), f.TimeBase)

}

func (f *Commons) SetTimeBase(v astiav.Rational) {
	f.TimeBase = v
}

func (f *Commons) GetDuration() int64 {
	return (*Commons)(f).Frame.Duration()
}

func (f *Commons) SetDuration(v int64) {
	(*Commons)(f).Frame.SetDuration(v)
}

func (f *Commons) GetPTS() int64 {
	return f.Frame.Pts()
}

func (f *Commons) GetDTS() int64 {
	return f.Frame.PktDts()
}

func (f *Commons) SetPTS(v int64) {
	f.Frame.SetPts(v)
}

func (f *Commons) SetDTS(v int64) {
	f.Frame.SetPktDts(v)
}

func (f *Commons) GetPTSAsDuration() time.Duration {
	return avconv.Duration(f.Frame.Pts(), f.TimeBase)
}

func (f *Commons) GetResolution() codectypes.Resolution {
	return codectypes.Resolution{
		Width:  uint32(f.Frame.Width()),
		Height: uint32(f.Frame.Height()),
	}
}
