package frame

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Input Commons

func BuildInput(
	f *astiav.Frame,
	codecParameters *astiav.CodecParameters,
	streamIndex, streamsCount int,
	streamDuration int64,
	timeBase astiav.Rational,
	pos int64,
	duration int64,
) Input {
	return Input{
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

func (f *Input) GetMediaType() astiav.MediaType {
	return (*Commons)(f).GetMediaType()
}

func (f *Input) GetTimeBase() astiav.Rational {
	return (*Commons)(f).GetTimeBase()
}

func (f *Input) GetSize() int {
	return (*Commons)(f).GetSize()
}

func (f *Input) GetStreamIndex() int {
	return (*Commons)(f).GetStreamIndex()
}

func (f *Input) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}

func (f *Input) GetDTSAsDuration() time.Duration {
	return (*Commons)(f).GetDTSAsDuration()
}

func (f *Input) GetPTS() int64 {
	return (*Commons)(f).GetPTS()
}

func (f *Input) GetPTSAsDuration() time.Duration {
	return (*Commons)(f).GetPTSAsDuration()
}

func (f *Input) GetStreamDurationAsDuration() time.Duration {
	return (*Commons)(f).GetStreamDurationAsDuration()
}
