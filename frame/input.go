package frame

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Input Commons

func BuildInput(
	frame *astiav.Frame,
	pos int64,
	duration int64,
	dts int64,
	fmt *astiav.FormatContext,
	s *astiav.Stream,
) Input {
	return Input{
		FormatContext: fmt,
		Stream:        s,
		Frame:         frame,
		Pos:           pos,
		Duration:      duration,
		DTS:           dts,
	}
}

func (f *Input) GetSize() int {
	return (*Commons)(f).GetSize()
}

func (f *Input) GetStreamIndex() int {
	return (*Commons)(f).GetStreamIndex()
}

func (f *Input) GetStream() *astiav.Stream {
	return (*Commons)(f).GetStream()
}

func (f *Input) GetFormatContext() *astiav.FormatContext {
	return (*Commons)(f).GetFormatContext()
}

func (f *Input) GetDurationAsDuration() time.Duration {
	return (*Commons)(f).GetDurationAsDuration()
}

func (f *Input) GetDTSAsDuration() time.Duration {
	return (*Commons)(f).GetDTSAsDuration()
}

func (f *Input) GetPTSAsDuration() time.Duration {
	return (*Commons)(f).GetPTSAsDuration()
}

func (f *Input) GetStreamDurationAsDuration() time.Duration {
	return (*Commons)(f).GetStreamDurationAsDuration()
}
