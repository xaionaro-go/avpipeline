// output.go defines the Output type for media frames to be sent to a sink.

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

func (f *Output) GetMediaType() astiav.MediaType { return (*Commons)(f).GetMediaType() }
func (f *Output) GetTimeBase() astiav.Rational   { return (*Commons)(f).GetTimeBase() }
func (f *Output) GetSize() int                   { return (*Commons)(f).GetSize() }
func (f *Output) GetStreamIndex() int            { return (*Commons)(f).GetStreamIndex() }
func (f *Output) SetStreamIndex(v int)           { (*Commons)(f).SetStreamIndex(v) }
func (f *Output) GetDuration() int64             { return (*Commons)(f).GetDuration() }
func (f *Output) SetDuration(v int64)            { (*Commons)(f).SetDuration(v) }
func (f *Output) GetPTS() int64                  { return (*Commons)(f).GetPTS() }
func (f *Output) GetDTS() int64                  { return (*Commons)(f).GetDTS() }
func (f *Output) SetPTS(v int64)                 { (*Commons)(f).SetPTS(v) }
func (f *Output) SetDTS(v int64)                 { (*Commons)(f).SetDTS(v) }
func (f *Output) SetTimeBase(v astiav.Rational)  { (*Commons)(f).SetTimeBase(v) }
func (f *Output) GetPipelineSideData() types.PipelineSideData {
	return (*Commons)(f).GetPipelineSideData()
}

func (f *Output) AddPipelineSideData(obj any) types.PipelineSideData {
	return (*Commons)(f).AddPipelineSideData(obj)
}
func (f *Output) IsKey() bool { return (*Commons)(f).IsKey() }
func (f *Output) GetCodecParameters() *astiav.CodecParameters {
	return (*Commons)(f).GetCodecParameters()
}
func (f *Output) GetResolution() codectypes.Resolution { return (*Commons)(f).GetResolution() }
func (f *Output) GetPTSAsDuration() time.Duration      { return (*Commons)(f).GetPTSAsDuration() }
func (f *Output) GetDTSAsDuration() time.Duration      { return (*Commons)(f).GetDTSAsDuration() }
func (f *Output) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}
