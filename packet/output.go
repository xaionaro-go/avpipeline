package packet

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

type Output Commons

func BuildOutput(
	pkt *astiav.Packet,
	streamInfo *StreamInfo,
) Output {
	return Output{
		Packet:     pkt,
		StreamInfo: streamInfo,
	}
}

func (o *Output) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}

func (o Output) String() string {
	return (Commons)(o).String()
}

func (o *Output) GetMediaType() astiav.MediaType {
	return (*Commons)(o).GetMediaType()
}

func (o *Output) GetTimeBase() astiav.Rational {
	return (*Commons)(o).GetTimeBase()
}

func (o *Output) SetTimeBase(v astiav.Rational) {
	(*Commons)(o).SetTimeBase(v)
}

func (o *Output) GetDuration() int64 {
	return (*Commons)(o).GetDuration()
}

func (o *Output) SetDuration(v int64) {
	(*Commons)(o).SetDuration(v)
}

func (o *Output) GetPTS() int64 {
	return (*Commons)(o).GetPTS()
}

func (o *Output) GetDTS() int64 {
	return (*Commons)(o).GetDTS()
}

func (o *Output) SetPTS(v int64) {
	(*Commons)(o).SetPTS(v)
}

func (o *Output) SetDTS(v int64) {
	(*Commons)(o).SetDTS(v)
}

func (o *Output) GetSize() int {
	return (*Commons)(o).GetSize()
}

func (o *Output) GetStreamIndex() int {
	return (*Commons)(o).GetStreamIndex()
}

func (o *Output) GetStream() *astiav.Stream {
	return (*Commons)(o).GetStream()
}

func (o *Output) GetStreamFromSource(ctx context.Context) *astiav.Stream {
	return (*Commons)(o).GetStreamFromSource(ctx)
}

func (o *Output) GetPipelineSideData() types.PipelineSideData {
	return (*Commons)(o).GetPipelineSideData()
}

func (o *Output) AddPipelineSideData(obj any) types.PipelineSideData {
	return (*Commons)(o).AddPipelineSideData(obj)
}

func (o *Output) IsKey() bool {
	return (*Commons)(o).IsKey()
}
