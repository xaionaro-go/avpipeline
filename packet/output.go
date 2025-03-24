package packet

import (
	"github.com/asticode/go-astiav"
)

type Output Commons

func BuildOutput(
	pkt *astiav.Packet,
	s *astiav.Stream,
	fmt *astiav.FormatContext,
) Output {
	return Output{
		Packet:        pkt,
		Stream:        s,
		FormatContext: fmt,
	}
}

func (o *Output) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}

func (o *Output) GetSize() int {
	return o.Packet.Size()
}

func (o *Output) GetStreamIndex() int {
	return o.Packet.StreamIndex()
}

func (o *Output) GetStream() *astiav.Stream {
	streamIndex := o.GetStreamIndex()
	for _, stream := range o.FormatContext.Streams() {
		if stream.Index() == streamIndex {
			return stream
		}
	}
	return nil
}

func (o *Output) GetFormatContext() *astiav.FormatContext {
	return o.FormatContext
}
