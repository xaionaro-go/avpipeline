// input.go defines the Input type for media packets received from a source.

package packet

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

type Input Commons

func BuildInput(
	pkt *astiav.Packet,
	streamInfo *StreamInfo,
) Input {
	return Input{
		Packet:     pkt,
		StreamInfo: streamInfo,
	}
}

func (pkt *Input) GetMediaType() astiav.MediaType { return (*Commons)(pkt).GetMediaType() }
func (pkt *Input) GetTimeBase() astiav.Rational   { return (*Commons)(pkt).GetTimeBase() }
func (pkt *Input) GetSize() int                   { return (*Commons)(pkt).GetSize() }
func (pkt *Input) GetStreamIndex() int            { return (*Commons)(pkt).GetStreamIndex() }
func (pkt *Input) SetStreamIndex(v int)           { (*Commons)(pkt).SetStreamIndex(v) }
func (pkt *Input) GetDuration() int64             { return (*Commons)(pkt).GetDuration() }
func (pkt *Input) SetDuration(v int64)            { (*Commons)(pkt).SetDuration(v) }
func (pkt *Input) GetPTS() int64                  { return (*Commons)(pkt).GetPTS() }
func (pkt *Input) GetDTS() int64                  { return (*Commons)(pkt).GetDTS() }
func (pkt *Input) SetPTS(v int64)                 { (*Commons)(pkt).SetPTS(v) }
func (pkt *Input) SetDTS(v int64)                 { (*Commons)(pkt).SetDTS(v) }
func (pkt *Input) SetTimeBase(v astiav.Rational)  { (*Commons)(pkt).SetTimeBase(v) }
func (pkt *Input) GetPipelineSideData() types.PipelineSideData {
	return (*Commons)(pkt).GetPipelineSideData()
}

func (pkt *Input) AddPipelineSideData(obj any) types.PipelineSideData {
	return (*Commons)(pkt).AddPipelineSideData(obj)
}
func (pkt *Input) IsKey() bool { return (*Commons)(pkt).IsKey() }
func (pkt *Input) IsOOBHeaders() bool {
	return (*Commons)(pkt).IsOOBHeaders()
}

func (pkt *Input) GetCodecParameters() *astiav.CodecParameters {
	return (*Commons)(pkt).GetCodecParameters()
}
func (pkt *Input) GetStream() *astiav.Stream { return (*Commons)(pkt).GetStream() }
func (pkt *Input) GetSource() Source         { return (*Commons)(pkt).GetSource() }
func (pkt *Input) GetStreamFromSource(ctx context.Context) *astiav.Stream {
	return (*Commons)(pkt).GetStreamFromSource(ctx)
}
func (pkt *Input) RescaleTS(src, dst astiav.Rational) { (*Commons)(pkt).RescaleTs(src, dst) }
