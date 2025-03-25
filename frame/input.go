package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Input Commons

func BuildInput(
	frame *astiav.Frame,
	dts int64,
	fmt *astiav.FormatContext,
	s *astiav.Stream,
) Input {
	return Input{
		FormatContext: fmt,
		Stream:        s,
		Frame:         frame,
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

func (f *Input) GetPTSAsDuration() time.Duration {
	return avconv.Duration(f.Frame.Pts(), f.Stream.TimeBase())
}
