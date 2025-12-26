package packet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/types"
)

type Source interface {
	fmt.Stringer
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
	*astiav.Stream   // is never nil
	Source           // is never nil
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
	*astiav.Packet // may be nil, if comes from NotifyAboutPacketSource instead of SendInputPacket
	*StreamInfo    // is never nil
}

func (pkt Commons) String() string {
	var params []string
	params = append(params,
		fmt.Sprintf("stream_index=%d", pkt.GetStreamIndex()),
		fmt.Sprintf("media_type=%s", pkt.GetMediaType()),
		fmt.Sprintf("pts=%d", pkt.GetPTS()),
		fmt.Sprintf("dts=%d", pkt.GetDTS()),
		fmt.Sprintf("size=%d", pkt.GetSize()),
		fmt.Sprintf("duration=%d", pkt.GetDuration()),
		fmt.Sprintf("is_key=%t", pkt.IsKey()),
	)
	sideData := pkt.Packet.SideData()
	if newExtraData, ok := sideData.NewExtraData().Get(); ok {
		params = append(params, fmt.Sprintf("new_extra_data=%s", extradata.Raw(newExtraData)))
	}
	return fmt.Sprintf("Packet{%s}", strings.Join(params, " "))
}

func (pkt *Commons) IsOOBHeaders() bool {
	if pkt.Stream == nil {
		return false
	}
	codecParams := pkt.Stream.CodecParameters()
	if codecParams == nil {
		return false
	}
	return len(codecParams.ExtraData()) > 0
}

func (pkt *Commons) GetSize() int {
	return pkt.Size()
}

func (pkt *Commons) GetMediaType() astiav.MediaType {
	if pkt.Stream == nil {
		return astiav.MediaTypeUnknown
	}
	codecParams := pkt.Stream.CodecParameters()
	if codecParams == nil {
		return astiav.MediaTypeUnknown
	}
	return codecParams.MediaType()
}

func (pkt *Commons) GetStreamIndex() int {
	if pkt.Packet != nil {
		return pkt.Packet.StreamIndex()
	}
	return pkt.Stream.Index()
}

func (pkt *Commons) GetStream() *astiav.Stream {
	return pkt.Stream
}

func (pkt *Commons) GetStreamFromSource(ctx context.Context) *astiav.Stream {
	streamIndex := pkt.GetStreamIndex()
	var result *astiav.Stream
	pkt.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		for _, stream := range fmtCtx.Streams() {
			if stream.Index() == streamIndex {
				result = stream
				return
			}
		}
	})
	return result
}

func (pkt *Commons) PtsAsDuration() time.Duration {
	return avconv.Duration(pkt.Pts(), pkt.Stream.TimeBase())
}

func (pkt *Commons) GetTimeBase() astiav.Rational {
	return pkt.Stream.TimeBase()
}

func (pkt *Commons) SetTimeBase(v astiav.Rational) {
	pkt.Stream.SetTimeBase(v)
}

func (pkt *Commons) GetDuration() int64 {
	return pkt.Packet.Duration()
}

func (pkt *Commons) SetDuration(v int64) {
	pkt.Packet.SetDuration(v)
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

func (pkt *Commons) GetPipelineSideData() types.PipelineSideData {
	return pkt.StreamInfo.PipelineSideData
}

func (pkt *Commons) AddPipelineSideData(obj any) types.PipelineSideData {
	pkt.PipelineSideData = append(pkt.PipelineSideData, obj)
	return pkt.PipelineSideData
}

func (pkt *Commons) GetSource() Source {
	return pkt.StreamInfo.Source
}

func (pkt *Commons) GetResolution() *codectypes.Resolution {
	if pkt.Stream == nil {
		return nil
	}
	codecParams := pkt.Stream.CodecParameters()
	if codecParams == nil {
		return nil
	}
	if codecParams.MediaType() != astiav.MediaTypeVideo {
		return nil
	}
	return &codectypes.Resolution{
		Width:  uint32(codecParams.Width()),
		Height: uint32(codecParams.Height()),
	}
}

func (pkt *Commons) IsKey() bool {
	if pkt.Packet == nil {
		return false
	}
	return pkt.Packet.Flags().Has(astiav.PacketFlagKey)
}

func (pkt *Commons) GetPacketSource() Source {
	return pkt.StreamInfo.Source
}
