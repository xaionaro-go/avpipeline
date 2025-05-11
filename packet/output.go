package packet

import (
	"context"

	"github.com/asticode/go-astiav"
)

type Output Commons

func BuildOutput(
	pkt *astiav.Packet,
	s *astiav.Stream,
	fmt Source,
) Output {
	return Output{
		Packet: pkt,
		Stream: s,
		Source: fmt,
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
