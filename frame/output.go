package frame

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Output Commons

func BuildOutput(
	f *astiav.Frame,
	codecParameters *astiav.CodecParameters,
	streamIndex, streamsCount int,
	streamDuration int64,
	timeBase astiav.Rational,
	pos int64,
	duration int64,
) Output {
	return Output{
		Frame:           f,
		CodecParameters: codecParameters,
		StreamIndex:     streamIndex,
		StreamsCount:    streamsCount,
		StreamDuration:  streamDuration,
		TimeBase:        timeBase,
		Pos:             pos,
		Duration:        duration,
	}
}

func (f *Output) GetMediaType() astiav.MediaType {
	return (*Commons)(f).GetMediaType()
}

func (f *Output) GetTimeBase() astiav.Rational {
	return (*Commons)(f).GetTimeBase()
}

func (f *Output) GetSize() int {
	return (*Commons)(f).GetSize()
}

func (f *Output) GetStreamIndex() int {
	return (*Commons)(f).GetStreamIndex()
}

func (f *Output) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}

func (f *Output) GetDTSAsDuration() time.Duration {
	return (*Commons)(f).GetDTSAsDuration()
}

func (f *Output) GetPTS() int64 {
	return (*Commons)(f).GetPTS()
}

func (f *Output) GetPTSAsDuration() time.Duration {
	return (*Commons)(f).GetPTSAsDuration()
}

func (f *Output) GetStreamDurationAsDuration() time.Duration {
	return (*Commons)(f).GetStreamDurationAsDuration()
}
