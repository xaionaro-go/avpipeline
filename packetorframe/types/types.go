package packetorframetypes

import (
	"fmt"

	"github.com/asticode/go-astiav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type AbstractSource interface {
	fmt.Stringer
}

type StreamInfo struct {
	*astiav.Stream
	Source           AbstractSource
	PipelineSideData globaltypes.PipelineSideData

	// Fallback fields if Stream is nil
	CodecParameters *astiav.CodecParameters
	StreamIndex     int
	StreamsCount    int
	TimeBase        astiav.Rational
	Duration        int64
}

func (si *StreamInfo) GetStreamIndex() int {
	if si.Stream != nil {
		return si.Stream.Index()
	}
	return si.StreamIndex
}

func (si *StreamInfo) GetTimeBase() astiav.Rational {
	if si.Stream != nil {
		return si.Stream.TimeBase()
	}
	return si.TimeBase
}

func (si *StreamInfo) GetCodecParameters() *astiav.CodecParameters {
	if si.Stream != nil {
		return si.Stream.CodecParameters()
	}
	return si.CodecParameters
}
