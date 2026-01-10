// types.go defines common types for packet-or-frame operations.

// Package packetorframetypes provides common types for packet-or-frame operations.
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
	TimeBase        astiav.Rational // TODO: remove this field, use Stream.TimeBase() instead
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

func (si *StreamInfo) GetMediaType() astiav.MediaType {
	if si.Stream != nil {
		return si.Stream.CodecParameters().MediaType()
	}
	if si.CodecParameters != nil {
		return si.CodecParameters.MediaType()
	}
	return astiav.MediaTypeUnknown
}
