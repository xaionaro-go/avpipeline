package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type Output Commons

func BuildOutput(
	f *astiav.Frame,
	streamInfo *StreamInfo,
) Output {
	return Output{
		Frame:      f,
		StreamInfo: streamInfo,
	}
}

func (f *Output) GetMediaType() astiav.MediaType {
	return (*Commons)(f).GetMediaType()
}

func (f *Output) GetTimeBase() astiav.Rational {
	return (*Commons)(f).GetTimeBase()
}

func (f *Output) GetSize() int {
	return (*Commons)(f).GetSize()
}

func (f *Output) GetStreamIndex() int {
	return (*Commons)(f).GetStreamIndex()
}

func (f *Output) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}

func (f *Output) GetDTSAsDuration() time.Duration {
	return (*Commons)(f).GetDTSAsDuration()
}

func (f *Output) SetTimeBase(v astiav.Rational) {
	(*Commons)(f).SetTimeBase(v)
}

func (f *Output) GetDuration() int64 {
	return (*Commons)(f).GetDuration()
}

func (f *Output) SetDuration(v int64) {
	(*Commons)(f).SetDuration(v)
}

func (f *Output) GetPTS() int64 {
	return (*Commons)(f).GetPTS()
}

func (f *Output) GetDTS() int64 {
	return (*Commons)(f).GetDTS()
}

func (f *Output) SetPTS(v int64) {
	(*Commons)(f).SetPTS(v)
}

func (f *Output) SetDTS(v int64) {
	(*Commons)(f).SetDTS(v)
}

func (f *Output) GetPTSAsDuration() time.Duration {
	return (*Commons)(f).GetPTSAsDuration()
}

func (f *Output) GetPipelineSideData() types.PipelineSideData {
	return f.PipelineSideData
}

func (f *Output) AddPipelineSideData(obj any) types.PipelineSideData {
	f.PipelineSideData = append(f.PipelineSideData, obj)
	return f.PipelineSideData
}

func (f *Output) Resolution() codectypes.Resolution {
	return (*Commons)(f).GetResolution()
}

func (f *Output) IsKey() bool {
	if f.Frame == nil {
		return false
	}
	return f.Frame.Flags().Has(astiav.FrameFlagKey)
}
