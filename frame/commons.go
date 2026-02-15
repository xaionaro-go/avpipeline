// commons.go provides common structures and methods for media frames.

package frame

import (
	"context"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type StreamInfo = packetorframetypes.StreamInfo

func BuildStreamInfo(
	source Source,
	codecParameters *astiav.CodecParameters,
	streamIndex, streamsCount int,
	timeBase astiav.Rational,
	duration int64,
	pipelineSideData types.PipelineSideData,
) *StreamInfo {
	return &StreamInfo{
		Source:           source,
		CodecParameters:  codecParameters,
		StreamIndex:      streamIndex,
		StreamsCount:     streamsCount,
		TimeBase:         timeBase,
		Duration:         duration,
		PipelineSideData: pipelineSideData,
	}
}

type Commons struct {
	*astiav.Frame
	Pos int64 // TODO: reuse pkt_pos from the frame
	*StreamInfo
}

func (f *Commons) GetMediaType() astiav.MediaType {
	return f.CodecParameters.MediaType()
}

func (f *Commons) GetStreamIndex() int {
	return f.StreamIndex
}

func (f *Commons) SetStreamIndex(v int) {
	f.StreamIndex = v
}

func (f *Commons) GetTimeBase() astiav.Rational {
	return f.StreamInfo.TimeBase
}

func (f *Commons) GetSize() int {
	if f.Frame == nil || f.StreamInfo == nil || f.CodecParameters == nil {
		return 0
	}
	ctx := context.Background()

	const align = 1

	switch f.CodecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		size, err := f.ImageBufferSize(align)
		if err != nil {
			return 0
		}
		return size
	case astiav.MediaTypeAudio:
		size, err := f.SamplesBufferSize(align)
		if err != nil {
			return 0
		}
		return size
	default:
		logger.Warnf(ctx, "Unsupported media type: %v", f.CodecParameters.MediaType())
		return 0 // TODO: implement this
	}
}

func (f *Commons) GetDurationAsDuration() time.Duration {
	return avconv.Duration(f.StreamInfo.Duration, f.StreamInfo.TimeBase)
}

func (f *Commons) GetDTSAsDuration() time.Duration {
	if f.Frame == nil {
		return 0
	}
	return avconv.Duration(f.PktDts(), f.StreamInfo.TimeBase)
}

func (f *Commons) SetTimeBase(v astiav.Rational) {
	f.StreamInfo.TimeBase = v
}

func (f *Commons) GetDuration() int64 {
	if f.Frame == nil {
		return 0
	}
	return f.Frame.Duration()
}

func (f *Commons) SetDuration(v int64) {
	if f.Frame == nil {
		return
	}
	f.Frame.SetDuration(v)
}

func (f *Commons) GetPTS() int64 {
	if f.Frame == nil {
		return astiav.NoPtsValue
	}
	return f.Frame.Pts()
}

func (f *Commons) GetDTS() int64 {
	if f.Frame == nil {
		return astiav.NoPtsValue
	}
	return f.Frame.PktDts()
}

func (f *Commons) SetPTS(v int64) {
	if f.Frame == nil {
		return
	}
	f.Frame.SetPts(v)
}

func (f *Commons) SetDTS(v int64) {
	if f.Frame == nil {
		return
	}
	f.Frame.SetPktDts(v)
}

func (f *Commons) GetPTSAsDuration() time.Duration {
	if f.Frame == nil {
		return 0
	}
	return avconv.Duration(f.Frame.Pts(), f.StreamInfo.TimeBase)
}

func (f *Commons) GetResolution() codectypes.Resolution {
	if f.Frame == nil {
		return codectypes.Resolution{}
	}
	return codectypes.Resolution{
		Width:  uint32(f.Frame.Width()),
		Height: uint32(f.Frame.Height()),
	}
}

func (f *Commons) IsKey() bool {
	if f.Frame == nil {
		return false
	}
	return f.Frame.Flags().Has(astiav.FrameFlagKey)
}

func (f *Commons) GetCodecParameters() *astiav.CodecParameters {
	return f.CodecParameters
}

func (f *Commons) GetPipelineSideData() types.PipelineSideData {
	return f.PipelineSideData
}

func (f *Commons) AddPipelineSideData(v any) types.PipelineSideData {
	f.PipelineSideData = append(f.PipelineSideData, v)
	return f.PipelineSideData
}
