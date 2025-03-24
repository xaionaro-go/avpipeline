package frame

import (
	"github.com/asticode/go-astiav"
)

type Output Commons

func BuildOutput(
	f *astiav.Frame,
	dts int64,
	s *astiav.Stream,
	fmt *astiav.FormatContext,
) Output {
	return Output{
		Frame:         f,
		Stream:        s,
		FormatContext: fmt,
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
