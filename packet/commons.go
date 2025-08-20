package packet

import (
	"context"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/types"
)

type Source interface {
	WithOutputFormatContext(context.Context, func(*astiav.FormatContext))
}

/* for easier copy&paste:

func () WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {

}

*/

type Sink interface {
	WithInputFormatContexter
	NotifyAboutPacketSourcer
}

type WithInputFormatContexter interface {
	WithInputFormatContext(context.Context, func(*astiav.FormatContext))
}

type NotifyAboutPacketSourcer interface {
	NotifyAboutPacketSource(context.Context, Source) error
}

/* for easier copy&paste:

func () WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {

}

func () NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {

}
*/

type StreamInfo struct {
	*astiav.Stream
	Source
	PipelineSideData types.PipelineSideData
}

func BuildStreamInfo(
	stream *astiav.Stream,
	source Source,
	pipelineSideData types.PipelineSideData,
) *StreamInfo {
	return &StreamInfo{
		Stream:           stream,
		Source:           source,
		PipelineSideData: pipelineSideData,
	}
}

type Commons struct {
	*astiav.Packet
	*StreamInfo
}

func (pkt *Commons) GetSize() int {
	return pkt.Size()
}

func (pkt *Commons) GetMediaType() astiav.MediaType {
	return pkt.Stream.CodecParameters().MediaType()
}

func (pkt *Commons) GetStreamIndex() int {
	return pkt.Stream.Index()
}

func (pkt *Commons) GetStream() *astiav.Stream {
	return pkt.Stream
}

func (pkt *Commons) PtsAsDuration() time.Duration {
	return avconv.Duration(pkt.Pts(), pkt.Stream.TimeBase())
}

func (pkt *Commons) GetPTS() int64 {
	return pkt.Packet.Pts()
}

func (pkt *Commons) GetDTS() int64 {
	return pkt.Packet.Dts()
}

func (pkt *Commons) SetPTS(v int64) {
	pkt.Packet.SetPts(v)
}

func (pkt *Commons) SetDTS(v int64) {
	pkt.Packet.SetDts(v)
}
