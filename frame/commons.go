package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/types"
)

type StreamInfo struct {
	CodecParameters  *astiav.CodecParameters // TODO: remove this from here
	StreamIndex      int
	StreamsCount     int
	StreamDuration   int64
	TimeBase         astiav.Rational // TODO: reuse the time_base from the frame
	Duration         int64           // TODO: reuse duration from the frame
	PipelineSideData types.PipelineSideData
}

func BuildStreamInfo(
	codecParameters *astiav.CodecParameters,
	streamIndex, streamsCount int,
	streamDuration int64,
	timeBase astiav.Rational,
	duration int64,
	pipelineSideData types.PipelineSideData,
) *StreamInfo {
	return &StreamInfo{
		CodecParameters:  codecParameters,
		StreamIndex:      streamIndex,
		StreamsCount:     streamsCount,
		StreamDuration:   streamDuration,
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

func (f *Commons) GetStreamDurationAsDuration() time.Duration {
	return avconv.Duration(f.StreamDuration, f.TimeBase)
}
