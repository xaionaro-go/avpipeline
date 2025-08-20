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

func (o *Output) GetMediaType() astiav.MediaType {
	return (*Commons)(o).GetMediaType()
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
	return o.Packet.Size()
}

func (o *Output) GetStreamIndex() int {
	return o.Packet.StreamIndex()
}

func (o *Output) GetStream(ctx context.Context) *astiav.Stream {
	streamIndex := o.GetStreamIndex()
	var result *astiav.Stream
	o.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		for _, stream := range fmtCtx.Streams() {
			if stream.Index() == streamIndex {
				result = stream
				return
			}
		}
	})
	return result
}

func (o *Output) GetPipelineSideData() types.PipelineSideData {
	return o.PipelineSideData
}
