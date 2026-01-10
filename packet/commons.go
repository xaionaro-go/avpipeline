// commons.go provides common structures and methods for media packets.

// Package packet provides types and utilities for handling media packets.
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
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
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

type StreamInfo = packetorframetypes.StreamInfo

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
	*astiav.Packet // may be nil, if comes from NotifyAboutPacketSource instead of SendInput
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
	if pkt.Packet != nil {
		sideData := pkt.Packet.SideData()
		if newExtraData, ok := sideData.NewExtraData().Get(); ok {
			params = append(params, fmt.Sprintf("new_extra_data=%s", extradata.Raw(newExtraData)))
		}
	}
	return fmt.Sprintf("Packet{%s}", strings.Join(params, " "))
}

func (pkt *Commons) IsOOBHeaders() bool {
	codecParams := pkt.GetCodecParameters()
	if codecParams == nil {
		return false
	}
	return len(codecParams.ExtraData()) > 0
}

func (pkt *Commons) GetSize() int {
	if pkt.Packet == nil {
		return 0
	}
	return pkt.Size()
}

func (pkt *Commons) GetMediaType() astiav.MediaType {
	return pkt.StreamInfo.GetMediaType()
}

func (pkt *Commons) GetStreamIndex() int {
	if pkt.Packet != nil {
		return pkt.Packet.StreamIndex()
	}
	return pkt.Stream.Index()
}

func (pkt *Commons) SetStreamIndex(v int) {
	if pkt.Packet == nil {
		return
	}
	pkt.Packet.SetStreamIndex(v)
}

func (pkt *Commons) GetStream() *astiav.Stream {
	if pkt.StreamInfo == nil {
		return nil
	}
	return pkt.StreamInfo.Stream
}

func (pkt *Commons) GetStreamFromSource(ctx context.Context) *astiav.Stream {
	streamIndex := pkt.GetStreamIndex()
	var result *astiav.Stream
	pkt.StreamInfo.Source.(Source).WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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
	return avconv.Duration(pkt.GetPTS(), pkt.GetTimeBase())
}

func (pkt *Commons) GetTimeBase() astiav.Rational {
	return pkt.StreamInfo.GetTimeBase()
}

func (pkt *Commons) SetTimeBase(v astiav.Rational) {
	if pkt.StreamInfo.Stream != nil {
		pkt.StreamInfo.Stream.SetTimeBase(v)
	}
	pkt.StreamInfo.TimeBase = v
}

func (pkt *Commons) GetDuration() int64 {
	if pkt.Packet == nil {
		return 0
	}
	return pkt.Packet.Duration()
}

func (pkt *Commons) SetDuration(v int64) {
	if pkt.Packet == nil {
		return
	}
	pkt.Packet.SetDuration(v)
}

func (pkt *Commons) GetPTS() int64 {
	if pkt.Packet == nil {
		return astiav.NoPtsValue
	}
	return pkt.Packet.Pts()
}

func (pkt *Commons) GetDTS() int64 {
	if pkt.Packet == nil {
		return astiav.NoPtsValue
	}
	return pkt.Packet.Dts()
}

func (pkt *Commons) SetPTS(v int64) {
	if pkt.Packet == nil {
		return
	}
	pkt.Packet.SetPts(v)
}

func (pkt *Commons) SetDTS(v int64) {
	if pkt.Packet == nil {
		return
	}
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
	return pkt.StreamInfo.Source.(Source)
}

func (pkt *Commons) GetResolution() *codectypes.Resolution {
	codecParams := pkt.GetCodecParameters()
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

func (pkt *Commons) GetCodecParameters() *astiav.CodecParameters {
	return pkt.StreamInfo.GetCodecParameters()
}

func (pkt *Commons) GetPacketSource() Source {
	return pkt.StreamInfo.Source.(Source)
}
