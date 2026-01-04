// input.go defines the Input type for media frames received from a source.

package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type Input Commons

func BuildInput(
	f *astiav.Frame,
	pos int64,
	streamInfo *StreamInfo,
) Input {
	return Input{
		Frame:      f,
		Pos:        pos,
		StreamInfo: streamInfo,
	}
}

func (f *Input) GetMediaType() astiav.MediaType { return (*Commons)(f).GetMediaType() }
func (f *Input) GetTimeBase() astiav.Rational   { return (*Commons)(f).GetTimeBase() }
func (f *Input) GetSize() int                   { return (*Commons)(f).GetSize() }
func (f *Input) GetStreamIndex() int            { return (*Commons)(f).GetStreamIndex() }
func (f *Input) SetStreamIndex(v int)           { (*Commons)(f).SetStreamIndex(v) }
func (f *Input) GetDuration() int64             { return (*Commons)(f).GetDuration() }
func (f *Input) SetDuration(v int64)            { (*Commons)(f).SetDuration(v) }
func (f *Input) GetPTS() int64                  { return (*Commons)(f).GetPTS() }
func (f *Input) GetDTS() int64                  { return (*Commons)(f).GetDTS() }
func (f *Input) SetPTS(v int64)                 { (*Commons)(f).SetPTS(v) }
func (f *Input) SetDTS(v int64)                 { (*Commons)(f).SetDTS(v) }
func (f *Input) SetTimeBase(v astiav.Rational)  { (*Commons)(f).SetTimeBase(v) }
func (f *Input) GetPipelineSideData() types.PipelineSideData {
	return (*Commons)(f).GetPipelineSideData()
}

func (f *Input) AddPipelineSideData(obj any) types.PipelineSideData {
	return (*Commons)(f).AddPipelineSideData(obj)
}
func (f *Input) IsKey() bool { return (*Commons)(f).IsKey() }
func (f *Input) GetCodecParameters() *astiav.CodecParameters {
	return (*Commons)(f).GetCodecParameters()
}
func (f *Input) GetResolution() codectypes.Resolution { return (*Commons)(f).GetResolution() }
func (f *Input) GetPTSAsDuration() time.Duration      { return (*Commons)(f).GetPTSAsDuration() }
func (f *Input) GetDTSAsDuration() time.Duration      { return (*Commons)(f).GetDTSAsDuration() }
func (f *Input) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}
