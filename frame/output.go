package frame

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Output Commons

func BuildOutput(
	f *astiav.Frame,
	pos int64,
	duration int64,
	dts int64,
	s *astiav.Stream,
	fmt *astiav.FormatContext,
) Output {
	return Output{
		Frame:         f,
		Stream:        s,
		FormatContext: fmt,
		Pos:           pos,
		Duration:      duration,
		DTS:           dts,
	}
}

func (f *Output) GetSize() int {
	return (*Commons)(f).GetSize()
}

func (f *Output) GetStreamIndex() int {
	return (*Commons)(f).GetStreamIndex()
}

func (f *Output) GetStream() *astiav.Stream {
	return (*Commons)(f).GetStream()
}

func (f *Output) GetFormatContext() *astiav.FormatContext {
	return (*Commons)(f).GetFormatContext()
}

func (f *Output) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}

func (f *Output) GetDTSAsDuration() time.Duration {
	return (*Commons)(f).GetDTSAsDuration()
}

func (f *Output) GetPTSAsDuration() time.Duration {
	return (*Commons)(f).GetPTSAsDuration()
}

func (f *Output) GetStreamDurationAsDuration() time.Duration {
	return (*Commons)(f).GetStreamDurationAsDuration()
}
