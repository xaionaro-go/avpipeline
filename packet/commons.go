package packet

import (
	"context"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Source interface {
	WithFormatContext(context.Context, func(*astiav.FormatContext))
	NotifyAboutPacketSource(context.Context, Source) error
}

/* for easier copy&paste:

func () WithFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {

}

func () NotifyAboutPacketSource(
	ctx context.Context,
	source Source,
) error {

}
*/

type Commons struct {
	*astiav.Packet
	*astiav.Stream
	Source
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
	return pkt.Pts()
}
